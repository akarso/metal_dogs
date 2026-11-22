package metaldogs

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/memory"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// PluginName is the stable plugin identity and Migrator namespace
// (schema versions plugin:metaldogs:<basename>).
const PluginName = "metaldogs"

// permJobsRead is core rbac.JobsRead ("jobs.read"); Host.RegisterAdminRoute takes the string.
const permJobsRead = "jobs.read"

// Plugin is the Metal Dogs Shopanda external plugin.
type Plugin struct {
	assets        ports.AssetStore
	uploads       *application.UploadService
	designs       *application.DesignService
	generate      *application.GenerateService
	worker        *application.GenerateWorker
	host          cartSurface
	syncJobNames  []string
	jobs          ports.JobAccess
	orderSource   ports.OrderSource
	snapshotClock ports.Clock
}

// cartSurface is the scoped pluginsdk.Host slice this plugin uses for cart and catalog.
// Init stores AsHostFor; Cart() is read on the request path because BindCartPort is late.
type cartSurface interface {
	Cart() (pluginsdk.CartPort, bool)
	Catalog() (pluginsdk.CatalogPort, bool)
	CatalogSeed() (pluginsdk.CatalogSeedPort, bool)
	BootstrapDB() (*sql.DB, bool)
	WithPluginWrite(ctx context.Context, pluginName string, fieldCodes ...string) context.Context
	Logger() pluginsdk.Logger
}

// Needs declares the composition-root ports this plugin reads.
// CapCatalogSeed stays declared so the seed CLI can read Host.CatalogSeed; serve and worker leave that port unbound.
// CapJobs and CapMedia are bound after Init on serve and worker. Plugin CLI does not bind them; pluginCLINeedsSkip skips both, so Require does not fail that boot.
func (p *Plugin) Needs() []pluginsdk.Capability {
	return []pluginsdk.Capability{
		pluginsdk.CapCart,
		pluginsdk.CapCatalog,
		pluginsdk.CapCatalogSeed,
		pluginsdk.CapDB,
		pluginsdk.CapJobs,
		pluginsdk.CapMedia,
	}
}

// New returns the Metal Dogs plugin.
func New() *Plugin { return &Plugin{snapshotClock: ports.SystemClock{}} }

// Name returns the stable plugin identity.
func (p *Plugin) Name() string { return PluginName }

// AssetStore is nil before Init and a wrapper afterwards, so non-nil does not mean media is bound.
// Each call resolves Host.Media, and a missing or unsigned backend fails that call.
func (p *Plugin) AssetStore() ports.AssetStore { return p.assets }

// UploadService returns the upload use case wired during Init (nil before Init,
// without media, or without Bootstrap.DB).
func (p *Plugin) UploadService() *application.UploadService { return p.uploads }

// DesignService returns the design use case wired during Init (nil before Init,
// without media, or without Bootstrap.DB).
func (p *Plugin) DesignService() *application.DesignService { return p.designs }

// GenerateService returns the generate use case wired during Init.
func (p *Plugin) GenerateService() *application.GenerateService { return p.generate }

// Init registers CLI, customizer routes, and sync jobs when the plugin is enabled.
// Media is a lazy Host.Media wrapper: the composition root binds storage after InitAll.
func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("metaldogs plugin: host not configured")
	}
	settings, err := SettingsFrom(h.ConfigValue)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return fmt.Errorf("metaldogs plugin: disabled (plugins.metaldogs.enabled=false)")
	}
	h.Require(pluginsdk.CapCart)
	h.Require(pluginsdk.CapCatalog)
	h.Require(pluginsdk.CapCatalogSeed)
	h.Require(pluginsdk.CapDB)
	h.Require(pluginsdk.CapJobs)
	h.Require(pluginsdk.CapMedia)
	p.host = h
	// ConfigValue is plugin-scoped; queue.driver is an allowlisted core key.
	if err := pluginsdk.RequireQueueDriver(h, "postgres"); err != nil {
		return fmt.Errorf("metaldogs plugin: %w", err)
	}
	if err := InstallGenerateJobFence(h); err != nil {
		return fmt.Errorf("metaldogs plugin: generate job fence: %w", err)
	}

	providerName := settings.Provider
	maxRetries := settings.MaxRetries
	maxAttempts := settings.MaxAttemptsPerDesign
	cooldownSecs := settings.CooldownSeconds

	if err := registerDesignIDField(h); err != nil {
		return fmt.Errorf("metaldogs plugin: design field: %w", err)
	}
	if err := registerCartValidation(h.Hooks(p.Name()), p); err != nil {
		return fmt.Errorf("metaldogs plugin: cart validation: %w", err)
	}
	registerDesignCheckout(h, p)

	registerCLICommands(h, p)

	log := h.Logger()
	store := lazyAssetStore{host: h}
	p.assets = store

	var templates *application.TemplateCatalog
	p.uploads = nil
	p.designs = nil
	p.generate = nil
	p.worker = nil
	p.syncJobNames = nil
	db, dbOK := h.BootstrapDB()
	if dbOK && db != nil {
		templates = &application.TemplateCatalog{Store: postgres.NewTemplateStore(db)}
		p.uploads = newUploadService(store, db, log)
		p.designs = newDesignService(store, db, templates, maxAttempts, cooldownSecs, log)
	}

	jobs := p.jobAccess(h)
	p.jobs = jobs
	if dbOK && db != nil {
		var genErr error
		p.generate, p.worker, genErr = newGenerateServices(jobs, store, db, providerName, maxRetries, maxAttempts, cooldownSecs, log)
		if genErr != nil {
			return fmt.Errorf("metaldogs plugin: generate services: %w", genErr)
		}
	}

	if err := mountCustomizerRoute(h, "GET /api/customizer/enabled", httpapi.Enabled()); err != nil {
		return fmt.Errorf("metaldogs plugin: enabled route: %w", err)
	}
	if err := mountCustomizerRoute(h, "GET /api/customizer/health", httpapi.Health(jobs)); err != nil {
		return fmt.Errorf("metaldogs plugin: health route: %w", err)
	}
	if err := mountCustomizerRoute(h, "GET /api/customizer/templates", httpapi.ListTemplates(templates)); err != nil {
		return fmt.Errorf("metaldogs plugin: templates list route: %w", err)
	}
	if err := mountCustomizerRoute(h, "GET /api/customizer/templates/{id}", httpapi.GetTemplate(templates)); err != nil {
		return fmt.Errorf("metaldogs plugin: templates get route: %w", err)
	}
	if err := mountCustomizerRoute(h, "POST /api/customizer/uploads", httpapi.CreateUpload(p.uploads)); err != nil {
		return fmt.Errorf("metaldogs plugin: uploads route: %w", err)
	}
	if err := mountCustomizerRoute(h, "POST /api/customizer/designs", httpapi.CreateDesign(p.designs)); err != nil {
		return fmt.Errorf("metaldogs plugin: designs create route: %w", err)
	}
	if err := mountCustomizerRoute(h, "GET /api/customizer/designs/{id}", httpapi.GetDesign(p.designs)); err != nil {
		return fmt.Errorf("metaldogs plugin: designs get route: %w", err)
	}
	if err := mountCustomizerRoute(h, "PATCH /api/customizer/designs/{id}", httpapi.PatchDesign(p.designs)); err != nil {
		return fmt.Errorf("metaldogs plugin: designs patch route: %w", err)
	}
	if err := mountCustomizerRoute(h, "POST /api/customizer/designs/{id}/generate", httpapi.GenerateDesign(p.generate)); err != nil {
		return fmt.Errorf("metaldogs plugin: designs generate route: %w", err)
	}
	if err := mountCustomizerRoute(h, "POST /api/customizer/designs/{id}/approve", httpapi.ApproveDesign(p.designs)); err != nil {
		return fmt.Errorf("metaldogs plugin: designs approve route: %w", err)
	}
	if err := mountCustomizerRoute(h, "POST /api/customizer/designs/{id}/clone", httpapi.CloneDesign(p.designs)); err != nil {
		return fmt.Errorf("metaldogs plugin: designs clone route: %w", err)
	}
	addToCart := &application.AddToCart{
		Designs: p.designs,
		Cart:    p,
	}
	if err := mountCustomizerRoute(h, "POST /api/customizer/designs/{id}/add-to-cart", httpapi.AddDesignToCart(addToCart)); err != nil {
		return fmt.Errorf("metaldogs plugin: designs add-to-cart route: %w", err)
	}
	if err := mountCustomizerRoute(h, "GET /api/customizer/generation-jobs/{id}", httpapi.GetGenerationJob(p.generate)); err != nil {
		return fmt.Errorf("metaldogs plugin: generation-jobs route: %w", err)
	}
	if err := h.RegisterAdminRoute("GET /api/v1/admin/customizer/health", permJobsRead, httpapi.AdminHealth(jobs)); err != nil {
		return fmt.Errorf("metaldogs plugin: admin health route: %w", err)
	}
	syncAPI := pluginsdk.NewHost(h, PluginName).Integration(PluginName)
	if err := registerObsPing(syncAPI, log); err != nil {
		return fmt.Errorf("metaldogs plugin: obs.ping: %w", err)
	}
	p.noteSyncJob(obsPingJob)
	if err := registerPrepareProduction(syncAPI, log); err != nil {
		return fmt.Errorf("metaldogs plugin: prepare_production_artwork: %w", err)
	}
	p.noteSyncJob(prepareProductionJobName)
	if p.designs != nil {
		p.designs.EnqueuePrepare = func(ctx context.Context, versionID, designID string) error {
			return enqueuePrepareProduction(jobs, ctx, versionID, designID)
		}
	}
	if p.worker != nil {
		if err := registerGenerate(syncAPI, p.worker, maxRetries); err != nil {
			return fmt.Errorf("metaldogs plugin: generate: %w", err)
		}
		p.noteSyncJob(generateJobName)
	}
	if settings.OrderSnapshotEnabled {
		if p.orderSource == nil {
			p.orderSource = memory.NewOrderSource()
		}
		if err := registerSnapshotOrder(syncAPI, p); err != nil {
			return fmt.Errorf("metaldogs plugin: snapshot_order_design: %w", err)
		}
		p.noteSyncJob(SnapshotOrderJobName)
	}
	return nil
}

func (p *Plugin) noteSyncJob(name string) {
	p.syncJobNames = append(p.syncJobNames, name)
}

func (p *Plugin) jobAccess(h pluginsdk.Host) ports.JobAccess {
	return &hostJobs{
		host:  h,
		sync:  pluginsdk.NewHost(h, PluginName).Integration(PluginName),
		names: &p.syncJobNames,
	}
}

func mountCustomizerRoute(h pluginsdk.Host, pattern string, handler http.Handler) error {
	return h.RegisterPublicRoute(pattern, handler, pluginsdk.NoCache())
}

func newUploadService(store ports.AssetStore, db *sql.DB, log pluginsdk.Logger) *application.UploadService {
	if store == nil || db == nil {
		return nil
	}
	svc := &application.UploadService{
		Assets: postgres.NewAssetRepo(db),
		Store:  store,
		IDs:    ports.UUIDGenerator{},
		Clock:  ports.SystemClock{},
	}
	if log != nil {
		svc.OnCleanupError = func(op, assetID, storageKey string, err error) {
			log.Error("metaldogs.upload.cleanup_failed", err, map[string]interface{}{
				"op":          op,
				"asset_id":    assetID,
				"storage_key": storageKey,
			})
		}
	}
	return svc
}

func newDesignService(store ports.AssetStore, db *sql.DB, templates *application.TemplateCatalog, maxAttempts, cooldownSecs int, log pluginsdk.Logger) *application.DesignService {
	if store == nil || db == nil || templates == nil {
		return nil
	}
	versions := postgres.NewDesignVersionRepo(db)
	if log != nil {
		versions.OnExtendMiss = func(role, assetID string) {
			log.Warn("metaldogs.approve.extend_delete_after_miss", map[string]interface{}{
				"role":     role,
				"asset_id": assetID,
			})
		}
	}
	svc := &application.DesignService{
		Designs:     postgres.NewDesignRepo(db),
		Assets:      postgres.NewAssetRepo(db),
		Jobs:        postgres.NewGenerationJobRepo(db),
		Versions:    versions,
		Templates:   templates,
		Store:       store,
		IDs:         ports.UUIDGenerator{},
		Clock:       ports.SystemClock{},
		MaxAttempts: maxAttempts,
		Cooldown:    time.Duration(cooldownSecs) * time.Second,
	}
	if log != nil {
		svc.OnCleanupError = func(op, assetID, storageKey string, err error) {
			log.Error("metaldogs.design.cleanup_failed", err, map[string]interface{}{
				"op":          op,
				"asset_id":    assetID,
				"storage_key": storageKey,
			})
		}
		svc.OnPrepareEnqueueError = func(designID, versionID string, err error) {
			log.Error("metaldogs.approve.enqueue_prepare_failed", err, map[string]interface{}{
				"design_id":  designID,
				"version_id": versionID,
			})
		}
	}
	return svc
}

func newGenerateServices(
	access ports.JobAccess,
	store ports.AssetStore,
	db *sql.DB,
	providerName string,
	maxRetries, maxAttempts, cooldownSecs int,
	log pluginsdk.Logger,
) (*application.GenerateService, *application.GenerateWorker, error) {
	if store == nil || db == nil || access == nil {
		return nil, nil, nil
	}
	prov, err := generation.Lookup(providerName)
	if err != nil {
		return nil, nil, err
	}
	jobs := postgres.NewGenerationJobRepo(db)
	designs := postgres.NewDesignRepo(db)
	assets := postgres.NewAssetRepo(db)
	tplStore := postgres.NewTemplateStore(db)
	worker := &application.GenerateWorker{
		Designs:          designs,
		Jobs:             jobs,
		Assets:           assets,
		Templates:        tplStore,
		Store:            store,
		Provider:         prov,
		IDs:              ports.UUIDGenerator{},
		Clock:            ports.SystemClock{},
		MaxQueueAttempts: maxRetries,
	}
	if log != nil {
		worker.OnCleanupError = func(op, assetID, storageKey string, err error) {
			log.Error("metaldogs.generate.cleanup_failed", err, map[string]interface{}{
				"op":          op,
				"asset_id":    assetID,
				"storage_key": storageKey,
			})
		}
	}
	enqueue := func(ctx context.Context, generationJobID, designID string) error {
		return enqueueGenerateIdempotent(access, ctx, generationJobID, designID)
	}
	svc := &application.GenerateService{
		Designs:     designs,
		Jobs:        jobs,
		Assets:      assets,
		IDs:         ports.UUIDGenerator{},
		Clock:       ports.SystemClock{},
		Enqueue:     enqueue,
		Provider:    prov.Name(),
		MaxAttempts: maxAttempts,
		Cooldown:    time.Duration(cooldownSecs) * time.Second, // 0 disables
	}
	return svc, worker, nil
}

// PlatformJobs adapts Host.Jobs and SyncJobs.Enqueue. names are the plugin's
// own sync job names (extapi.SyncJobType); nil names yields no admin types.
// A nil host returns a nil interface.
func PlatformJobs(h pluginsdk.Host, names []string) ports.JobAccess {
	if h == nil {
		return nil
	}
	copied := append([]string(nil), names...)
	return &hostJobs{
		host:  h,
		sync:  pluginsdk.NewHost(h, PluginName).Integration(PluginName),
		names: &copied,
	}
}

type hostJobs struct {
	host  pluginsdk.Host
	sync  *pluginsdk.SyncJobs
	names *[]string
}

func (j *hostJobs) port() (pluginsdk.JobsPort, bool) {
	if j == nil || j.host == nil {
		return nil, false
	}
	return j.host.Jobs()
}

func (j *hostJobs) QueueBound() bool {
	_, ok := j.port()
	return ok
}

func (j *hostJobs) ReaderBound() bool {
	return j.QueueBound()
}

func (j *hostJobs) JobTypes(pluginSlug string) []string {
	if j == nil || j.names == nil || pluginSlug != PluginName {
		return nil
	}
	var out []string
	for _, name := range *j.names {
		jobType, err := extapi.SyncJobType(PluginName, name)
		if err != nil {
			continue
		}
		out = append(out, jobType)
	}
	return out
}

func (j *hostJobs) CountsByType(ctx context.Context, jobType string) (map[ports.JobStatus]int, error) {
	port, ok := j.port()
	if !ok {
		return nil, fmt.Errorf("metaldogs: queue unbound")
	}
	return port.CountsByType(ctx, jobType)
}

func (j *hostJobs) Store() (ports.JobStore, bool) {
	port, ok := j.port()
	if !ok {
		return nil, false
	}
	return jobsPortStore{port: port}, true
}

func (j *hostJobs) DB() *sql.DB {
	if j == nil || j.host == nil {
		return nil
	}
	db, ok := j.host.BootstrapDB()
	if !ok {
		return nil
	}
	return db
}

func (j *hostJobs) EnqueueManual(ctx context.Context, name, jobID string, payload map[string]interface{}) error {
	if j == nil || j.sync == nil {
		return fmt.Errorf("metaldogs: jobs access required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return j.sync.Enqueue(ctx, name, payload, pluginsdk.WithJobID(jobID))
}

type jobsPortStore struct {
	port pluginsdk.JobsPort
}

func (s jobsPortStore) Get(ctx context.Context, id string) (*ports.JobDetail, error) {
	return s.port.Get(ctx, id)
}

func (s jobsPortStore) Requeue(ctx context.Context, id string, attempt int, opts ...ports.RequeueOption) error {
	return s.port.Requeue(ctx, id, attempt, opts...)
}
