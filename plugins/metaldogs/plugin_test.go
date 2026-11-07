package metaldogs_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs"
)

// fakeSigned is a minimal SignedURLStorage for Init wiring (no *localfs.Storage).
type fakeSigned struct {
	objects map[string][]byte
}

func (f *fakeSigned) Name() string { return "fake" }
func (f *fakeSigned) Save(path string, file io.Reader) error {
	b, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	if f.objects == nil {
		f.objects = map[string][]byte{}
	}
	f.objects[path] = b
	return nil
}
func (f *fakeSigned) Delete(path string) error {
	delete(f.objects, path)
	return nil
}
func (f *fakeSigned) URL(string) string { return "" }
func (f *fakeSigned) Head(_ context.Context, path string) error {
	if _, ok := f.objects[path]; !ok {
		return os.ErrNotExist
	}
	return nil
}
func (f *fakeSigned) SignedURL(_ context.Context, path string, _ media.SignURLOptions) (string, error) {
	if _, ok := f.objects[path]; !ok {
		return "", os.ErrNotExist
	}
	return "https://signed.example/" + path, nil
}

var _ media.SignedURLStorage = (*fakeSigned)(nil)

func testApp(t *testing.T, enabled bool) *plugin.App {
	t.Helper()
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{
			Plugins: config.PluginsConfig{
				MetalDogs: config.MetalDogsPluginConfig{Enabled: enabled},
			},
		},
	}
	if enabled {
		app.RegisterMediaStorage(&fakeSigned{})
	}
	return app
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
	if err := metaldogs.New().Init(testApp(t, false)); err == nil {
		t.Fatal("Init() expected error when plugins.metaldogs.enabled is false")
	}
}

func TestPlugin_Init_RequiresMediaStorage(t *testing.T) {
	t.Parallel()
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{
			Plugins: config.PluginsConfig{
				MetalDogs: config.MetalDogsPluginConfig{Enabled: true},
			},
		},
	}
	if err := metaldogs.New().Init(app); err == nil {
		t.Fatal("Init() expected error without media storage")
	}
}

func TestPlugin_Init_Enabled_RegistersRoute(t *testing.T) {
	t.Parallel()
	app := testApp(t, true)
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
	if patterns["GET /api/customizer/dev-assets/{token}"] {
		t.Fatal("dev-assets shim must not be registered after platform SignedURL absorb")
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
		t.Fatalf("enabled status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v body=%s", err, rec.Body.String())
	}
	if body["status"] != "ok" || body["plugin"] != "metaldogs" {
		t.Fatalf("body = %#v", body)
	}
}
