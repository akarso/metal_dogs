package metaldogs

import (
	"fmt"

	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// PluginName is the stable plugin identity and Migrator namespace
// (schema versions plugin:metaldogs:<basename>).
const PluginName = "metaldogs"

// Plugin is the Metal Dogs Shopanda external plugin.
type Plugin struct {
	assets ports.AssetStore
}

// New returns the Metal Dogs plugin.
func New() *Plugin { return &Plugin{} }

// Name returns the stable plugin identity.
func (p *Plugin) Name() string { return PluginName }

// AssetStore returns the private object store wired during Init (nil before Init).
func (p *Plugin) AssetStore() ports.AssetStore { return p.assets }

// Init registers public HTTP routes, obs.ping, and the AssetStore when
// plugins.metaldogs.enabled is true.
// Registration in cmd/api/register_plugins.go is also gated on that flag.
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

	store, err := resolveAssetStore(app)
	if err != nil {
		return fmt.Errorf("metaldogs plugin: asset store: %w", err)
	}
	p.assets = store

	if err := app.RegisterPublicRoute("GET /api/customizer/enabled", httpapi.Enabled()); err != nil {
		return fmt.Errorf("metaldogs plugin: enabled route: %w", err)
	}
	if err := app.RegisterPublicRoute("GET /api/customizer/health", httpapi.Health(app)); err != nil {
		return fmt.Errorf("metaldogs plugin: health route: %w", err)
	}
	if err := app.RegisterAdminRoute("GET /api/v1/admin/customizer/health", rbac.JobsRead, httpapi.AdminHealth(app)); err != nil {
		return fmt.Errorf("metaldogs plugin: admin health route: %w", err)
	}
	if err := registerObsPing(app); err != nil {
		return fmt.Errorf("metaldogs plugin: obs.ping: %w", err)
	}
	registerCLICommands(app)
	return nil
}
