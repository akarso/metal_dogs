package metaldogs_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/cli"
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

// initMetalDogs runs Init via Registry.InitAll so app.cliRegistry is configured
// (RegisterCommand panics on a bare App used in older unit tests).
func initMetalDogs(t *testing.T, app *plugin.App) *metaldogs.Plugin {
	t.Helper()
	reg := plugin.NewRegistry(app.Logger)
	p := metaldogs.New()
	reg.Register(p)
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll summary = %+v, want 1 initialized", summary)
	}
	return p
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

func TestPlugin_Init_WithoutMedia_EvalOnly_NoHTTP(t *testing.T) {
	t.Parallel()
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	p := metaldogs.New()
	reg.Register(p)
	log := logger.NewWithWriter(io.Discard, "warn")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{
			Plugins: config.PluginsConfig{
				MetalDogs: config.MetalDogsPluginConfig{Enabled: true},
			},
		},
	}
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll summary = %+v", summary)
	}
	if p.AssetStore() != nil {
		t.Fatal("AssetStore must stay nil without media")
	}
	if _, ok := reg.CLIRegistry().Get(metaldogs.CommandEval); !ok {
		t.Fatal("metaldogs:eval must register without media")
	}
	if len(app.PublicRoutes()) != 0 || len(app.AdminRoutes()) != 0 {
		t.Fatalf("HTTP routes without media = public %#v admin %#v", app.PublicRoutes(), app.AdminRoutes())
	}
	if len(app.SyncJobs()) != 0 {
		t.Fatalf("SyncJobs without media = %#v", app.SyncJobs())
	}
}

func TestPlugin_Init_Enabled_RegistersRoute(t *testing.T) {
	t.Parallel()
	// InitAll wires app.cliRegistry before Init (RegisterCommand panics otherwise).
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	p := metaldogs.New()
	reg.Register(p)
	app := testApp(t, true)
	if p.AssetStore() != nil {
		t.Fatal("AssetStore before Init must be nil")
	}
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll summary = %+v, want 1 initialized", summary)
	}
	if p.AssetStore() == nil {
		t.Fatal("AssetStore after Init must be set")
	}
	routes := app.PublicRoutes()
	patterns := map[string]bool{}
	for _, r := range routes {
		patterns[r.Pattern] = true
	}
	if !patterns["GET /api/customizer/enabled"] || !patterns["GET /api/customizer/health"] ||
		!patterns["GET /api/customizer/templates"] || !patterns["GET /api/customizer/templates/{id}"] ||
		!patterns["POST /api/customizer/uploads"] {
		t.Fatalf("PublicRoutes = %+v, want enabled + health + templates + uploads", routes)
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
	for _, sj := range app.SyncJobs() {
		if sj.JobType == "integration.sync.metaldogs.obs.ping" {
			foundPing = true
		}
	}
	if !foundPing {
		t.Fatalf("SyncJobs missing obs.ping: %+v", app.SyncJobs())
	}
	cmds := reg.CLIRegistry().List()
	foundEval := false
	for _, c := range cmds {
		if c.Name == metaldogs.CommandEval {
			foundEval = true
			if c.RequiresDB {
				t.Fatal("metaldogs:eval must not require DB")
			}
		}
	}
	if !foundEval {
		t.Fatalf("CLIRegistry missing %s: %+v", metaldogs.CommandEval, cmds)
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

func TestCLI_Eval_FakeProvider(t *testing.T) {
	t.Parallel()
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(metaldogs.New())
	app := testApp(t, true)
	if summary := reg.InitAll(app); summary.Failed > 0 {
		t.Fatalf("InitAll: %+v", summary)
	}
	if _, ok := reg.CLIRegistry().Get(metaldogs.CommandTemplates); !ok {
		t.Fatal("metaldogs:templates not registered")
	}
	cmd, ok := reg.CLIRegistry().Get(metaldogs.CommandEval)
	if !ok {
		t.Fatal("metaldogs:eval not registered")
	}

	root := t.TempDir()
	tpl := filepath.Join(root, "t.json")
	photos := filepath.Join(root, "photos")
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(photos, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tpl, []byte(`{"id":"cli","prompt":"p","prompt_version":"v1","parameters":{"width":4,"height":4,"seed":1}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(photos, "a.jpg"), []byte{0xff, 0xd8, 0xff, 0xd9}, 0o640); err != nil {
		t.Fatal(err)
	}

	err := cmd.Run(cli.Context{Ctx: context.Background(), Logger: app.Logger, Config: app.Config}, []string{
		"-template", tpl,
		"-photos", photos,
		"-out", out,
		"-provider", "fake",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "report.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCLI_Eval_NonZeroOnSampleFailure(t *testing.T) {
	t.Parallel()
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(metaldogs.New())
	app := testApp(t, true)
	if summary := reg.InitAll(app); summary.Failed > 0 {
		t.Fatalf("InitAll: %+v", summary)
	}
	cmd, ok := reg.CLIRegistry().Get(metaldogs.CommandEval)
	if !ok {
		t.Fatal("metaldogs:eval not registered")
	}

	root := t.TempDir()
	tpl := filepath.Join(root, "t.json")
	photos := filepath.Join(root, "photos")
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(photos, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tpl, []byte(`{"id":"cli","prompt":"p","parameters":{"width":4,"height":4}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(photos, "empty.jpg"), nil, 0o640); err != nil {
		t.Fatal(err)
	}

	err := cmd.Run(cli.Context{Ctx: context.Background(), Logger: app.Logger, Config: app.Config}, []string{
		"-template", tpl,
		"-photos", photos,
		"-out", out,
	})
	if err == nil {
		t.Fatal("expected non-zero exit when samples fail")
	}
}
