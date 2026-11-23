package metaldogs_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/hooks"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"

	_ "github.com/jackc/pgx/v5/stdlib"
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
func (f *fakeSigned) Read(_ context.Context, path string) ([]byte, error) {
	b, ok := f.objects[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}

var _ media.SignedURLStorage = (*fakeSigned)(nil)
var _ media.ReadableStorage = (*fakeSigned)(nil)

func metalDogsPlugins(enabled bool, extra map[string]string) config.PluginsConfig {
	if !enabled && len(extra) == 0 {
		return config.PluginsConfig{}
	}
	kv := map[string]string{}
	for k, v := range extra {
		kv[k] = v
	}
	if enabled {
		kv["enabled"] = "true"
	}
	return config.PluginsConfig{Extra: map[string]map[string]string{"metaldogs": kv}}
}

func testApp(t *testing.T, enabled bool) *plugin.App {
	t.Helper()
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{Plugins: metalDogsPlugins(enabled, nil)},
	}
	if enabled {
		app.RegisterMediaStorage(&fakeSigned{})
	}
	app.SetPermissionRegistry(rbac.NewRegistry())
	return app
}

// initMetalDogs runs Init via Registry.InitAll so app.cliRegistry is configured
// (RegisterCommand panics on a bare App used in older unit tests).
func initMetalDogs(t *testing.T, app *plugin.App) *metaldogs.Plugin {
	t.Helper()
	reg := plugin.NewRegistry(app.Logger)
	p := metaldogs.New()
	reg.Register(pluginsdk.Adapt(p))
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

func TestPlugin_Init_NonPostgresQueue(t *testing.T) {
	t.Parallel()
	app := testApp(t, true)
	app.Config.Queue.Driver = "rabbitmq"
	err := pluginsdk.Adapt(metaldogs.New()).Init(app)
	if err == nil || !strings.Contains(err.Error(), `queue.driver="rabbitmq"`) {
		t.Fatalf("Init: %v", err)
	}
}

func TestPlugin_Init_Disabled(t *testing.T) {
	t.Parallel()
	if err := pluginsdk.Adapt(metaldogs.New()).Init(testApp(t, false)); err == nil {
		t.Fatal("Init() expected error when plugins.metaldogs.enabled is false")
	}
}

func TestPlugin_Init_CooldownSecondsBounds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		secs    int
		wantErr string
	}{
		{0, ""},
		{30, ""},
		{86400, ""},
		{-1, "cooldown_seconds must be >= 0"},
		{86401, "cooldown_seconds must be <="},
	}
	for _, tc := range cases {
		log := logger.NewWithWriter(io.Discard, "error")
		app := &plugin.App{
			Logger: log,
			Bus:    event.NewBus(log),
			Config: &config.Config{
				Plugins: metalDogsPlugins(true, map[string]string{
					"generation_cooldown_seconds": strconv.Itoa(tc.secs),
				}),
			},
		}
		if tc.wantErr != "" {
			err := pluginsdk.Adapt(metaldogs.New()).Init(app)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("secs=%d: want err containing %q, got %v", tc.secs, tc.wantErr, err)
			}
			continue
		}
		reg := plugin.NewRegistry(log)
		p := metaldogs.New()
		reg.Register(pluginsdk.Adapt(p))
		summary := reg.InitAll(app)
		if summary.Failed > 0 {
			t.Fatalf("secs=%d: InitAll failed %+v", tc.secs, summary)
		}
	}
}

func TestPlugin_Init_ProductionSpec(t *testing.T) {
	t.Parallel()
	log := logger.NewWithWriter(io.Discard, "error")
	incomplete := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{Plugins: metalDogsPlugins(true, map[string]string{
			"production_spec.width_px": "32",
		})},
	}
	err := pluginsdk.Adapt(metaldogs.New()).Init(incomplete)
	if err == nil || !strings.Contains(err.Error(), "production_spec.height_px") {
		t.Fatalf("Init err = %v", err)
	}

	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{Plugins: metalDogsPlugins(true, map[string]string{
			"production_spec.width_px":             "32",
			"production_spec.height_px":            "32",
			"production_spec.dpi":                  "72",
			"production_spec.require_transparency": "false",
			"production_spec.color_profile":        "srgb",
			"production_spec.upscale_policy":       "reject",
		})},
	}
	app.RegisterMediaStorage(&fakeSigned{})
	app.SetPermissionRegistry(rbac.NewRegistry())
	reg := plugin.NewRegistry(log)
	reg.Register(pluginsdk.Adapt(metaldogs.New()))
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("complete spec InitAll = %+v", summary)
	}
}

func TestPlugin_Init_WithoutMedia_RoutesRegisteredMediaResolvedLazily(t *testing.T) {
	t.Parallel()
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	p := metaldogs.New()
	reg.Register(pluginsdk.Adapt(p))
	log := logger.NewWithWriter(io.Discard, "warn")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{Plugins: metalDogsPlugins(true, nil)},
	}
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll summary = %+v", summary)
	}
	if p.AssetStore() == nil {
		t.Fatal("AssetStore wrapper must be set; media resolves at use")
	}
	if err := p.AssetStore().Head(context.Background(), "private/blobs/metaldogs/source/2026/11/x.jpg"); err == nil || !strings.Contains(err.Error(), "media storage not registered") {
		t.Fatalf("unbound media: %v", err)
	}
	if _, ok := reg.CLIRegistry().Get(metaldogs.CommandEval); !ok {
		t.Fatal("metaldogs:eval must register without media")
	}
	if len(app.PublicRoutes()) == 0 {
		t.Fatal("customizer routes register before BindMediaStorage")
	}
	if len(app.SyncJobs()) == 0 {
		t.Fatal("sync jobs register before BindMediaStorage")
	}
}

type plainMedia struct{}

func (plainMedia) Name() string                 { return "plain" }
func (plainMedia) Save(string, io.Reader) error { return nil }
func (plainMedia) Delete(string) error          { return nil }
func (plainMedia) URL(string) string            { return "" }

func TestPlugin_UnsignedMediaFailsOnUseHealthOmitsIt(t *testing.T) {
	t.Parallel()
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	p := metaldogs.New()
	reg.Register(pluginsdk.Adapt(p))
	log := logger.NewWithWriter(io.Discard, "warn")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{Plugins: metalDogsPlugins(true, nil)},
	}
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll summary = %+v", summary)
	}
	if err := app.BindMediaStorage(plainMedia{}); err != nil {
		t.Fatal(err)
	}
	err := p.AssetStore().Head(context.Background(), "private/blobs/metaldogs/source/2026/11/x.jpg")
	if err == nil || !strings.Contains(err.Error(), "does not implement SignedURLStorage") || strings.Contains(err.Error(), "MediaBackend") {
		t.Fatalf("unsigned backend: %v", err)
	}
	var health http.Handler
	for _, r := range app.PublicRoutes() {
		if r.Pattern == "GET /api/customizer/health" {
			health = r.Handler
		}
	}
	if health == nil {
		t.Fatal("health route missing")
	}
	rec := httptest.NewRecorder()
	health.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/health", nil))
	if strings.Contains(rec.Body.String(), "SignedURLStorage") || strings.Contains(rec.Body.String(), "MediaBackend") {
		t.Fatalf("health reports media: %s", rec.Body.String())
	}
}

func TestPlugin_Init_Enabled_RegistersRoute(t *testing.T) {
	t.Parallel()
	// InitAll wires app.cliRegistry before Init (RegisterCommand panics otherwise).
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	p := metaldogs.New()
	reg.Register(pluginsdk.Adapt(p))
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
	if p.UploadService() != nil {
		t.Fatal("UploadService must stay nil without Bootstrap.DB")
	}
	routes := app.PublicRoutes()
	patterns := map[string]bool{}
	for _, r := range routes {
		patterns[r.Pattern] = true
	}
	if !patterns["GET /api/customizer/enabled"] || !patterns["GET /api/customizer/health"] ||
		!patterns["GET /api/customizer/templates"] || !patterns["GET /api/customizer/templates/{id}"] ||
		!patterns["POST /api/customizer/uploads"] ||
		!patterns["POST /api/customizer/designs"] || !patterns["GET /api/customizer/designs/{id}"] ||
		!patterns["PATCH /api/customizer/designs/{id}"] ||
		!patterns["POST /api/customizer/designs/{id}/generate"] ||
		!patterns["POST /api/customizer/designs/{id}/add-to-cart"] ||
		!patterns["GET /api/customizer/generation-jobs/{id}"] {
		t.Fatalf("PublicRoutes = %+v, want enabled + health + templates + uploads + designs + generate", routes)
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

func TestPlugin_Init_Enabled_RegistersObsPing(t *testing.T) {
	t.Parallel()
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	p := metaldogs.New()
	reg.Register(pluginsdk.Adapt(p))
	app := testApp(t, true)
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll summary = %+v", summary)
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
		if c.Name == metaldogs.CommandTemplates && !c.RequiresDB {
			t.Fatal("metaldogs:templates must require DB so load/publish/retire see BootstrapDB")
		}
	}
	if !foundEval {
		t.Fatalf("CLIRegistry missing %s: %+v", metaldogs.CommandEval, cmds)
	}
}

func TestCLI_Eval_FakeProvider(t *testing.T) {
	t.Parallel()
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(pluginsdk.Adapt(metaldogs.New()))
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
	reg.Register(pluginsdk.Adapt(metaldogs.New()))
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

func TestCLI_TemplatesLoad_UsesBootstrapDB(t *testing.T) {
	t.Parallel()
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(pluginsdk.Adapt(metaldogs.New()))
	app := testApp(t, true)
	if summary := reg.InitAll(app); summary.Failed > 0 {
		t.Fatalf("InitAll: %+v", summary)
	}
	if !app.CapabilityDeclaredBy(plugin.CapDB, metaldogs.PluginName) {
		t.Fatal("CapDB must be declared at Init so a later Bootstrap.DB is visible")
	}
	cmd, ok := reg.CLIRegistry().Get(metaldogs.CommandTemplates)
	if !ok || !cmd.RequiresDB {
		t.Fatalf("templates cmd = %+v", cmd)
	}
	_, src, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate plugin_test.go")
	}
	path := filepath.Join(filepath.Dir(src), "templates", "head-front-blackmetal.yaml")
	err := cmd.Run(cli.Context{Ctx: context.Background()}, []string{"load", "-file", path})
	if err == nil || !strings.Contains(err.Error(), "database is not configured") {
		t.Fatalf("load without DB: %v", err)
	}
	conn, err := sql.Open("pgx", "postgres://127.0.0.1:1/nope?sslmode=disable&connect_timeout=2")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	app.Bootstrap = &plugin.Bootstrap{DB: conn}
	err = cmd.Run(cli.Context{Ctx: context.Background()}, []string{"load", "-file", path})
	if err == nil || !strings.Contains(err.Error(), "metaldogs templates scan:") || !strings.Contains(err.Error(), "connect") {
		t.Fatalf("load with Bootstrap.DB: %v, want a store scan of that pool", err)
	}
}

func TestPlugin_Init_Enabled_RegistersDesignIDField(t *testing.T) {
	t.Parallel()
	for _, withMedia := range []bool{false, true} {
		log := logger.NewWithWriter(io.Discard, "error")
		app := &plugin.App{
			Logger: log,
			Bus:    event.NewBus(log),
			Config: &config.Config{Plugins: metalDogsPlugins(true, nil)},
		}
		if withMedia {
			app.RegisterMediaStorage(&fakeSigned{})
		}
		reg := plugin.NewRegistry(log)
		reg.Register(pluginsdk.Adapt(metaldogs.New()))
		summary := reg.InitAll(app)
		if summary.Failed > 0 || summary.Initialized != 1 {
			t.Fatalf("media=%v InitAll summary = %+v", withMedia, summary)
		}
		field, ok := app.ExtensionRegistry().Get("metaldogs.design_id")
		if !ok {
			t.Fatalf("media=%v: metaldogs.design_id is not registered", withMedia)
		}
		if app.ExtensionRegistry().Owner("metaldogs.design_id") != metaldogs.PluginName {
			t.Fatalf("media=%v: owner = %q", withMedia, app.ExtensionRegistry().Owner("metaldogs.design_id"))
		}
		if field.Visibility != domainext.VisibilityPublic {
			t.Fatalf("visibility = %q", field.Visibility)
		}
		if !field.Access.PluginOnlyWrite {
			t.Fatal("field is writable by the public cart API")
		}
		if field.StorageMode != domainext.StorageSnapshot {
			t.Fatalf("storage = %q", field.StorageMode)
		}
		if field.Scope != domainext.TargetCartItem || field.Type != domainext.FieldTypeString {
			t.Fatalf("scope=%q type=%q", field.Scope, field.Type)
		}
		re, err := regexp.Compile(field.Validation.Regex)
		if err != nil {
			t.Fatalf("regex: %v", err)
		}
		if !re.MatchString("11111111-1111-4111-8111-111111111111") {
			t.Fatalf("regex %q rejected a UUID", field.Validation.Regex)
		}
		if re.MatchString("not-a-uuid") || re.MatchString("") {
			t.Fatalf("regex %q accepted a non-UUID", field.Validation.Regex)
		}
	}
}

func TestPlugin_Init_Disabled_DoesNotRegisterDesignIDField(t *testing.T) {
	t.Parallel()
	app := testApp(t, false)
	app.SetHookRegistry(hooks.NewRegistry(app.Logger))
	if err := pluginsdk.Adapt(metaldogs.New()).Init(app); err == nil {
		t.Fatal("Init() expected error when plugins.metaldogs.enabled is false")
	}
	if _, ok := app.ExtensionRegistry().Get("metaldogs.design_id"); ok {
		t.Fatal("metaldogs.design_id registered while the plugin is disabled")
	}
	reg := app.HookRegistry()
	if reg == nil {
		t.Fatal("hook registry missing")
	}
	for _, entry := range reg.Catalog() {
		if entry.Name == string(extapi.HookCartValidate) {
			t.Fatal("cart validation registered while the plugin is disabled")
		}
	}
}

func TestPlugin_Init_Enabled_RegistersCartValidation(t *testing.T) {
	t.Parallel()
	app := testApp(t, true)
	initMetalDogs(t, app)
	reg := app.HookRegistry()
	if reg == nil {
		t.Fatal("hook registry missing")
	}
	var handlers int
	for _, entry := range reg.Catalog() {
		if entry.Name != string(extapi.HookCartValidate) {
			continue
		}
		handlers = len(entry.Handlers)
		if handlers != 1 || entry.Handlers[0].Registrant != metaldogs.PluginName {
			t.Fatalf("handlers=%+v", entry.Handlers)
		}
	}
	if handlers != 1 {
		t.Fatal("cart validation is not registered")
	}
}
