package metaldogs

import (
	"fmt"

	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
)

// PluginName is the stable plugin identity and Migrator namespace
// (schema versions plugin:metaldogs:<basename>).
const PluginName = "metaldogs"

// Plugin is the Metal Dogs Shopanda external plugin.
type Plugin struct{}

// New returns the Metal Dogs plugin.
func New() *Plugin { return &Plugin{} }

// Name returns the stable plugin identity.
func (p *Plugin) Name() string { return PluginName }

// Init registers public HTTP routes and the obs.ping sync job when
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
	return nil
}
