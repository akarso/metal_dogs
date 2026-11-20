package metaldogs

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
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
	assets   ports.AssetStore
	uploads  *application.UploadService
	designs  *application.DesignService
	generate *application.GenerateService
	worker   *application.GenerateWorker
	host     cartSurface
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

// Needs declares CapCart, CapCatalog, and CapCatalogSeed.
// CapCatalogSeed stays declared so the seed CLI can read Host.CatalogSeed; serve and worker leave that port unbound.
func (p *Plugin) Needs() []plugin.Capability {
	return []plugin.Capability{plugin.CapCart, plugin.CapCatalog, plugin.CapCatalogSeed}
}

// New returns the Metal Dogs plugin.
func New() *Plugin { return &Plugin{} }

// Name returns the stable plugin identity.
func (p *Plugin) Name() string { return PluginName }

// AssetStore returns the private object store wired during Init (nil before Init
// or when Init ran in eval-only mode without media).
func (p *Plugin) AssetStore() ports.AssetStore { return p.assets }

// UploadService returns the upload use case wired during Init (nil before Init,
// without media, or without Bootstrap.DB).
func (p *Plugin) UploadService() *application.UploadService { return p.uploads }

// DesignService returns the design use case wired during Init (nil before Init,
// without media, or without Bootstrap.DB).
func (p *Plugin) DesignService() *application.DesignService { return p.designs }

// GenerateService returns the generate use case wired during Init.
func (p *Plugin) GenerateService() *application.GenerateService { return p.generate }

// Init registers CLI always when enabled. HTTP routes, obs.ping, and AssetStore
// require media SignedURLStorage (F4 fail-closed). Without media, Init still
// succeeds so offline metaldogs:eval works, but no customizer HTTP is mounted.
func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("metaldogs plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("metaldogs plugin: config not configured")
	}
	h := pluginsdk.AsHostFor(app, PluginName)
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
	p.host = h
	// CLI opens the DB after InitAll freezes needs. BootstrapDB stays false
	// unless CapDB is recorded here, including when Bootstrap.DB is still nil.
	h.Require(pluginsdk.CapDB)
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
	if err := registerCartValidation(app, p); err != nil {
		return fmt.Errorf("metaldogs plugin: cart validation: %w", err)
	}

	registerCLICommands(h, p)

	store, err := resolveAssetStore(app)
	if err != nil {
		p.assets = nil
		if app.Logger != nil {
			app.Logger.Warn("metaldogs: media unavailable; eval CLI only (no customizer HTTP / AssetStore)", map[string]interface{}{
				"error": err.Error(),
			})
		}
		return nil
	}
	p.assets = store

	// Wire catalog/uploads/designs/generate once at Init when Bootstrap.DB is present.
	var templates *application.TemplateCatalog
	p.uploads = nil
	p.designs = nil
	p.generate = nil
	p.worker = nil
	if app.Bootstrap != nil && app.Bootstrap.DB != nil {
		templates = &application.TemplateCatalog{Store: postgres.NewTemplateStore(app.Bootstrap.DB)}
		p.uploads = newUploadService(store, app.Bootstrap.DB, app.Logger)
		p.designs = newDesignService(store, app.Bootstrap.DB, templates, maxAttempts, cooldownSecs, app.Logger)
		var genErr error
		p.generate, p.worker, genErr = newGenerateServices(app, store, app.Bootstrap.DB, providerName, maxRetries, maxAttempts, cooldownSecs, app.Logger)
		if genErr != nil {
			return fmt.Errorf("metaldogs plugin: generate services: %w", genErr)
		}
	}

	if err := mountCustomizerRoute(h, "GET /api/customizer/enabled", httpapi.Enabled()); err != nil {
		return fmt.Errorf("metaldogs plugin: enabled route: %w", err)
	}
	jobs := PlatformJobs(app)
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
	if err := registerObsPing(app); err != nil {
		return fmt.Errorf("metaldogs plugin: obs.ping: %w", err)
	}
	syncAPI := pluginsdk.New(app, PluginName).Integration(PluginName)
	if err := registerPrepareProduction(syncAPI, app.Logger); err != nil {
		return fmt.Errorf("metaldogs plugin: prepare_production_artwork: %w", err)
	}
	if p.designs != nil {
		p.designs.EnqueuePrepare = func(ctx context.Context, versionID, designID string) error {
			return enqueuePrepareProduction(jobs, ctx, versionID, designID)
		}
	}
	// Register generate even when worker is nil (no DB) so SyncJobs list is complete;
	// handler no-ops safely only when worker non-nil — require worker for registration.
	if p.worker != nil {
		if err := registerGenerate(syncAPI, p.worker, maxRetries); err != nil {
			return fmt.Errorf("metaldogs plugin: generate: %w", err)
		}
	}
	return nil
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
	app *plugin.App,
	store ports.AssetStore,
	db *sql.DB,
	providerName string,
	maxRetries, maxAttempts, cooldownSecs int,
	log pluginsdk.Logger,
) (*application.GenerateService, *application.GenerateWorker, error) {
	if store == nil || db == nil || app == nil {
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
	access := PlatformJobs(app)
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

type appJobs struct {
	app *plugin.App
}

// PlatformJobs returns a nil interface when app is nil.
func PlatformJobs(app *plugin.App) ports.JobAccess {
	if app == nil {
		return nil
	}
	return appJobs{app: app}
}

func (a appJobs) bound() (any, bool) {
	if a.app == nil {
		return nil, false
	}
	q, ok := a.app.BoundQueue()
	if !ok || q == nil {
		return nil, false
	}
	return q, true
}

func (a appJobs) QueueBound() bool {
	_, ok := a.bound()
	return ok
}

type queueReaderSurface interface {
	Get(context.Context, string) (*ports.JobDetail, error)
	CountsByStatus(context.Context) (map[ports.JobStatus]int, error)
	CountsByType(context.Context, string) (map[ports.JobStatus]int, error)
}

func (a appJobs) ReaderBound() bool {
	q, ok := a.bound()
	if !ok {
		return false
	}
	_, ok = q.(queueReaderSurface)
	return ok
}

func (a appJobs) JobTypes(pluginSlug string) []string {
	if a.app == nil {
		return nil
	}
	var out []string
	for _, reg := range a.app.SyncJobs() {
		if reg.PluginSlug == pluginSlug {
			out = append(out, reg.JobType)
		}
	}
	return out
}

func (a appJobs) CountsByType(ctx context.Context, jobType string) (map[ports.JobStatus]int, error) {
	q, ok := a.bound()
	if !ok {
		return nil, fmt.Errorf("metaldogs: queue unbound")
	}
	c, ok := q.(queueReaderSurface)
	if !ok {
		return nil, fmt.Errorf("metaldogs: queue reader unavailable")
	}
	return c.CountsByType(ctx, jobType)
}

func (a appJobs) Store() (ports.JobStore, bool) {
	q, ok := a.bound()
	if !ok {
		return nil, false
	}
	s, ok := q.(ports.JobStore)
	return s, ok
}

func (a appJobs) DB() *sql.DB {
	if a.app == nil || a.app.Bootstrap == nil {
		return nil
	}
	return a.app.Bootstrap.DB
}

func (a appJobs) EnqueueManual(ctx context.Context, name, jobID string, payload map[string]interface{}) error {
	if a.app == nil {
		return fmt.Errorf("metaldogs: app required")
	}
	queue, ok := a.app.BoundQueue()
	if !ok || queue == nil {
		return extapi.ErrSyncJobQueueUnbound
	}
	jobType, err := extapi.SyncJobType(PluginName, name)
	if err != nil {
		return err
	}
	var reg plugin.SyncJobRegistration
	found := false
	for _, r := range a.app.SyncJobs() {
		if r.JobType == jobType {
			reg = r
			found = true
			break
		}
	}
	if !found {
		return extapi.ErrSyncJobNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload = plugin.InjectRequestID(ctx, payload)
	job, err := plugin.NewSyncJobEnvelope(reg, extapi.SyncTriggerManual, payload)
	if err != nil {
		return fmt.Errorf("envelope: %w", err)
	}
	job.ID = jobID
	return queue.Enqueue(ctx, job)
}
