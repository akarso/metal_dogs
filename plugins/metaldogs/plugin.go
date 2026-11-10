package metaldogs

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// PluginName is the stable plugin identity and Migrator namespace
// (schema versions plugin:metaldogs:<basename>).
const PluginName = "metaldogs"

// Plugin is the Metal Dogs Shopanda external plugin.
type Plugin struct {
	assets   ports.AssetStore
	uploads  *application.UploadService
	designs  *application.DesignService
	generate *application.GenerateService
	worker   *application.GenerateWorker
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
	if !app.Config.Plugins.MetalDogs.Enabled {
		return fmt.Errorf("metaldogs plugin: disabled (plugins.metaldogs.enabled=false)")
	}

	genCfg := app.Config.Plugins.MetalDogs.Generation
	providerName := strings.TrimSpace(genCfg.Provider)
	if providerName == "" {
		providerName = "fake"
	}
	if providerName != "fake" {
		return fmt.Errorf("metaldogs plugin: generation.provider %q not supported in C4 (use fake)", providerName)
	}
	maxRetries := genCfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	maxAttempts := genCfg.MaxAttemptsPerDesign
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	registerCLICommands(app)

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
		p.designs = newDesignService(store, app.Bootstrap.DB, templates, app.Logger)
		var genErr error
		p.generate, p.worker, genErr = newGenerateServices(app, store, app.Bootstrap.DB, providerName, maxRetries, maxAttempts)
		if genErr != nil {
			return fmt.Errorf("metaldogs plugin: generate services: %w", genErr)
		}
	}

	if err := app.RegisterPublicRoute("GET /api/customizer/enabled", httpapi.Enabled()); err != nil {
		return fmt.Errorf("metaldogs plugin: enabled route: %w", err)
	}
	if err := app.RegisterPublicRoute("GET /api/customizer/health", httpapi.Health(app)); err != nil {
		return fmt.Errorf("metaldogs plugin: health route: %w", err)
	}
	if err := app.RegisterPublicRoute("GET /api/customizer/templates", httpapi.ListTemplates(templates)); err != nil {
		return fmt.Errorf("metaldogs plugin: templates list route: %w", err)
	}
	if err := app.RegisterPublicRoute("GET /api/customizer/templates/{id}", httpapi.GetTemplate(templates)); err != nil {
		return fmt.Errorf("metaldogs plugin: templates get route: %w", err)
	}
	if err := app.RegisterPublicRoute("POST /api/customizer/uploads", httpapi.CreateUpload(p.uploads)); err != nil {
		return fmt.Errorf("metaldogs plugin: uploads route: %w", err)
	}
	if err := app.RegisterPublicRoute("POST /api/customizer/designs", httpapi.CreateDesign(p.designs)); err != nil {
		return fmt.Errorf("metaldogs plugin: designs create route: %w", err)
	}
	if err := app.RegisterPublicRoute("GET /api/customizer/designs/{id}", httpapi.GetDesign(p.designs)); err != nil {
		return fmt.Errorf("metaldogs plugin: designs get route: %w", err)
	}
	if err := app.RegisterPublicRoute("PATCH /api/customizer/designs/{id}", httpapi.PatchDesign(p.designs)); err != nil {
		return fmt.Errorf("metaldogs plugin: designs patch route: %w", err)
	}
	if err := app.RegisterPublicRoute("POST /api/customizer/designs/{id}/generate", httpapi.GenerateDesign(p.generate)); err != nil {
		return fmt.Errorf("metaldogs plugin: designs generate route: %w", err)
	}
	if err := app.RegisterPublicRoute("GET /api/customizer/generation-jobs/{id}", httpapi.GetGenerationJob(p.generate)); err != nil {
		return fmt.Errorf("metaldogs plugin: generation-jobs route: %w", err)
	}
	if err := app.RegisterAdminRoute("GET /api/v1/admin/customizer/health", rbac.JobsRead, httpapi.AdminHealth(app)); err != nil {
		return fmt.Errorf("metaldogs plugin: admin health route: %w", err)
	}
	if err := registerObsPing(app); err != nil {
		return fmt.Errorf("metaldogs plugin: obs.ping: %w", err)
	}
	// Register generate even when worker is nil (no DB) so SyncJobs list is complete;
	// handler no-ops safely only when worker non-nil — require worker for registration.
	if p.worker != nil {
		if err := registerGenerate(app, p.worker, maxRetries); err != nil {
			return fmt.Errorf("metaldogs plugin: generate: %w", err)
		}
	}
	return nil
}

func newUploadService(store ports.AssetStore, db *sql.DB, log logger.Logger) *application.UploadService {
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

func newDesignService(store ports.AssetStore, db *sql.DB, templates *application.TemplateCatalog, log logger.Logger) *application.DesignService {
	if store == nil || db == nil || templates == nil {
		return nil
	}
	svc := &application.DesignService{
		Designs:   postgres.NewDesignRepo(db),
		Assets:    postgres.NewAssetRepo(db),
		Templates: templates,
		Store:     store,
		IDs:       ports.UUIDGenerator{},
		Clock:     ports.SystemClock{},
	}
	if log != nil {
		svc.OnCleanupError = func(op, assetID, storageKey string, err error) {
			log.Error("metaldogs.design.cleanup_failed", err, map[string]interface{}{
				"op":          op,
				"asset_id":    assetID,
				"storage_key": storageKey,
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
	maxRetries, maxAttempts int,
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
	enqueue := func(ctx context.Context, generationJobID, designID string) error {
		return enqueueGenerateIdempotent(app, ctx, generationJobID, designID)
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
	}
	return svc, worker, nil
}
