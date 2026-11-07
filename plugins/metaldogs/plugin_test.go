package metaldogs_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs"
)

func testApp(enabled bool) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{
			Plugins: config.PluginsConfig{
				MetalDogs: config.MetalDogsPluginConfig{Enabled: enabled},
			},
		},
	}
}

func TestPlugin_Name(t *testing.T) {
	t.Parallel()
	if got := metaldogs.New().Name(); got != metaldogs.PluginName {
		t.Fatalf("Name() = %q, want %q", got, metaldogs.PluginName)
	}
}

func TestPlugin_Migration_ImplementsMigrator(t *testing.T) {
	t.Parallel()
	var _ plugin.Migrator = metaldogs.New()
	spec := metaldogs.New().Migration()
	if spec.FS == nil || spec.Dir != "migrations" {
		t.Fatalf("Migration() = %+v, want embed FS and dir migrations", spec)
	}
}

func TestPlugin_Init_Disabled(t *testing.T) {
	t.Parallel()
	if err := metaldogs.New().Init(testApp(false)); err == nil {
		t.Fatal("Init() expected error when plugins.metaldogs.enabled is false")
	}
}

func TestPlugin_Init_Enabled_RegistersRoute(t *testing.T) {
	t.Parallel()
	app := testApp(true)
	p := metaldogs.New()
	if p.AssetStore() != nil {
		t.Fatal("AssetStore before Init must be nil")
	}
	if err := p.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.AssetStore() == nil {
		t.Fatal("AssetStore after Init must be set")
	}
	routes := app.PublicRoutes()
	patterns := map[string]bool{}
	for _, r := range routes {
		patterns[r.Pattern] = true
	}
	if !patterns["GET /api/customizer/enabled"] || !patterns["GET /api/customizer/health"] {
		t.Fatalf("PublicRoutes = %+v, want enabled + health", routes)
	}
	if !patterns["GET /api/customizer/dev-assets/{token}"] {
		t.Fatalf("PublicRoutes = %+v, want local dev-assets shim", routes)
	}
	admins := app.AdminRoutes()
	foundAdmin := false
	for _, r := range admins {
		if r.Pattern == "GET /api/v1/admin/customizer/health" {
			foundAdmin = true
			if r.Permission != rbac.JobsRead {
				t.Fatalf("admin health perm = %q, want %q", r.Permission, rbac.JobsRead)
			}
		}
	}
	if !foundAdmin {
		t.Fatalf("AdminRoutes = %+v, want admin customizer health", admins)
	}
	foundPing := false
	for _, reg := range app.SyncJobs() {
		if reg.JobType == "integration.sync.metaldogs.obs.ping" {
			foundPing = true
		}
	}
	if !foundPing {
		t.Fatalf("SyncJobs missing obs.ping: %+v", app.SyncJobs())
	}
	var enabled plugin.PublicRoute
	for _, r := range routes {
		if r.Pattern == "GET /api/customizer/enabled" {
			enabled = r
		}
	}
	rec := httptest.NewRecorder()
	enabled.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/enabled", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if body := rec.Body.String(); body != `{"status":"ok","plugin":"metaldogs"}` {
		t.Fatalf("body = %q", body)
	}
}
