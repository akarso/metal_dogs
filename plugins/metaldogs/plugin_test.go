package metaldogs_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
	if err := metaldogs.New().Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	routes := app.PublicRoutes()
	if len(routes) != 1 || routes[0].Pattern != "GET /api/customizer/enabled" {
		t.Fatalf("PublicRoutes = %+v", routes)
	}
	rec := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/enabled", nil))
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
