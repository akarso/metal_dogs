package metaldogs

import (
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// PluginName is the stable plugin identity and Migrator namespace
// (schema versions plugin:metaldogs:<basename>).
const PluginName = "metaldogs"

// Plugin is the Metal Dogs Shopanda external plugin.
type Plugin struct {
	assets  ports.AssetStore
	uploads *application.UploadService
	designs *application.DesignService
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

	// Wire catalog/uploads/designs once at Init when Bootstrap.DB is present. Nil deps →
	// handlers return 503 (unit tests register routes without DB).
	var templates *application.TemplateCatalog
	p.uploads = nil
	p.designs = nil
	if app.Bootstrap != nil && app.Bootstrap.DB != nil {
		templates = &application.TemplateCatalog{Store: postgres.NewTemplateStore(app.Bootstrap.DB)}
		p.uploads = newUploadService(store, app.Bootstrap.DB, app.Logger)
		p.designs = newDesignService(store, app.Bootstrap.DB, templates)
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
	if err := app.RegisterAdminRoute("GET /api/v1/admin/customizer/health", rbac.JobsRead, httpapi.AdminHealth(app)); err != nil {
		return fmt.Errorf("metaldogs plugin: admin health route: %w", err)
	}
	if err := registerObsPing(app); err != nil {
		return fmt.Errorf("metaldogs plugin: obs.ping: %w", err)
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

func newDesignService(store ports.AssetStore, db *sql.DB, templates *application.TemplateCatalog) *application.DesignService {
	if store == nil || db == nil || templates == nil {
		return nil
	}
	return &application.DesignService{
		Designs:   postgres.NewDesignRepo(db),
		Assets:    postgres.NewAssetRepo(db),
		Templates: templates,
		Store:     store,
		IDs:       ports.UUIDGenerator{},
		Clock:     ports.SystemClock{},
	}
}
