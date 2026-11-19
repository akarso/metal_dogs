package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/interfaces/http/shared"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs"
)

type stubSignedMedia struct{}

func (stubSignedMedia) Name() string                       { return "stub" }
func (stubSignedMedia) Save(string, io.Reader) error       { return nil }
func (stubSignedMedia) Delete(string) error                { return nil }
func (stubSignedMedia) URL(string) string                  { return "" }
func (stubSignedMedia) Head(context.Context, string) error { return os.ErrNotExist }
func (stubSignedMedia) SignedURL(context.Context, string, media.SignURLOptions) (string, error) {
	return "", os.ErrNotExist
}

var _ media.SignedURLStorage = stubSignedMedia{}

func findPlugin(t *testing.T, registry *plugin.Registry, name string) plugin.Plugin {
	t.Helper()
	for _, e := range registry.Entries() {
		if e.Name == name {
			return e.Plugin
		}
	}
	return nil
}

func TestRegisterPlugins_MetalDogs_Disabled_NotRegistered(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{} // MetalDogs.Enabled defaults false
	log := logger.NewWithWriter(io.Discard, "error")
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)

	if p := findPlugin(t, registry, metaldogs.PluginName); p != nil {
		t.Fatal("metaldogs registered when plugins.metaldogs.enabled is false")
	}
	regs, err := registry.CollectMigrations(cfg.Plugins.DependsOn)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range regs {
		if r.PluginName == metaldogs.PluginName {
			t.Fatalf("CollectMigrations included metaldogs while disabled: %+v", regs)
		}
	}

	app := &plugin.App{Logger: log, Bus: event.NewBus(log), Config: cfg}
	mux := http.NewServeMux()
	for _, route := range app.PublicRoutes() {
		mux.Handle(route.Pattern, route.Handler)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/enabled", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when registerPlugins skips metaldogs", rec.Code)
	}
}

func TestRegisterPlugins_MetalDogs_BadEnabled_InitFails(t *testing.T) {
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
	t.Setenv("SHOPANDA_AUTH_JWT_SECRET", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("SHOPANDA_DEV_MODE", "true")
	t.Setenv("SHOPANDA_MEDIA_LOCAL_SIGN_SECRET", "unit-test-media-local-sign-secret-32b")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_ENABLED", "maybe")

	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("server:\n  host: 127.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	res, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	log := logger.NewWithWriter(io.Discard, "error")
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, res.Config)
	if findPlugin(t, registry, metaldogs.PluginName) == nil {
		t.Fatal("metaldogs not registered for a bad enabled value")
	}
	app := &plugin.App{Logger: log, Bus: event.NewBus(log), Config: res.Config}
	summary := registry.InitAll(app)
	if summary.Failed == 0 {
		t.Fatalf("InitAll summary = %+v, want failure", summary)
	}
	var initErr error
	for _, e := range registry.Entries() {
		if e.Name == metaldogs.PluginName {
			initErr = e.Err
		}
	}
	if initErr == nil || !strings.Contains(initErr.Error(), "is not a bool") {
		t.Fatalf("err = %v", initErr)
	}
}

func TestCollectMigrations_MetalDogs_NoInit(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Extra: map[string]map[string]string{
				"metaldogs": {"enabled": "true"},
			},
			B2B: config.B2BPluginConfig{Enabled: false},
		},
	}
	log := logger.NewWithWriter(io.Discard, "error")
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	regs, err := registry.CollectMigrations(cfg.Plugins.DependsOn)
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 1 || regs[0].PluginName != "metaldogs" {
		t.Fatalf("CollectMigrations = %+v, want only metaldogs", regs)
	}
	spec := metaldogs.New().Migration()
	if regs[0].Dir != spec.Dir {
		t.Fatalf("Dir = %q, want %q", regs[0].Dir, spec.Dir)
	}
}

func TestRegisterPlugins_MetalDogs_Enabled_RouteAndMigrator(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Extra: map[string]map[string]string{
				"metaldogs": {"enabled": "true"},
			},
		},
	}
	log := logger.NewWithWriter(io.Discard, "error")
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)

	p := findPlugin(t, registry, metaldogs.PluginName)
	if p == nil {
		t.Fatal("metaldogs not registered when enabled")
	}

	regs, err := registry.CollectMigrations(cfg.Plugins.DependsOn)
	if err != nil {
		t.Fatal(err)
	}
	foundMig := false
	for _, r := range regs {
		if r.PluginName == metaldogs.PluginName {
			foundMig = true
			if r.Dir != "migrations" {
				t.Fatalf("Dir = %q", r.Dir)
			}
		}
	}
	if !foundMig {
		t.Fatalf("CollectMigrations = %+v, want metaldogs entry", regs)
	}

	app := &plugin.App{Logger: log, Bus: event.NewBus(log), Config: cfg}
	app.RegisterMediaStorage(stubSignedMedia{})
	// InitAll wires cliRegistry before Init (RegisterCommand for metaldogs:eval).
	// Use a fresh registry so other registerPlugins entries do not fail Init.
	initReg := plugin.NewRegistry(log)
	initReg.Register(p)
	summary := initReg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll summary = %+v", summary)
	}
	routes := app.PublicRoutes()
	patterns := map[string]bool{}
	for _, r := range routes {
		patterns[r.Pattern] = true
	}
	if patterns["GET /api/customizer"] || patterns["HEAD /api/customizer"] || patterns["/api/customizer"] {
		t.Fatalf("PublicRoutes = %+v, exact /api/customizer must not be registered", routes)
	}
	if !patterns["GET /api/customizer/enabled"] || !patterns["GET /api/customizer/health"] ||
		!patterns["GET /api/customizer/templates"] || !patterns["GET /api/customizer/templates/{id}"] ||
		!patterns["POST /api/customizer/uploads"] ||
		!patterns["POST /api/customizer/designs"] || !patterns["GET /api/customizer/designs/{id}"] ||
		!patterns["PATCH /api/customizer/designs/{id}"] ||
		!patterns["POST /api/customizer/designs/{id}/generate"] ||
		!patterns["GET /api/customizer/generation-jobs/{id}"] {
		t.Fatalf("PublicRoutes = %+v, want enabled + health + templates + uploads + designs + generate", routes)
	}
	foundAdminHealth := false
	for _, r := range app.AdminRoutes() {
		if r.Pattern == "GET /api/v1/admin/customizer/health" {
			foundAdminHealth = true
			if r.Permission != rbac.JobsRead {
				t.Fatalf("admin health perm = %q, want %q", r.Permission, rbac.JobsRead)
			}
		}
	}
	if !foundAdminHealth {
		t.Fatalf("AdminRoutes = %+v, want admin customizer health", app.AdminRoutes())
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
		t.Fatalf("status = %d", rec.Code)
	}
	foundPing := false
	for _, reg := range app.SyncJobs() {
		if reg.JobType == "integration.sync.metaldogs.obs.ping" {
			foundPing = true
		}
	}
	if !foundPing {
		t.Fatalf("SyncJobs = %+v, want obs.ping", app.SyncJobs())
	}

	var customizer []plugin.PublicRoute
	for _, r := range routes {
		if !strings.Contains(r.Pattern, "/api/customizer/") {
			continue
		}
		if !r.NoCache {
			t.Fatalf("customizer route %q registered without NoCache", r.Pattern)
		}
		customizer = append(customizer, r)
	}
	if len(customizer) == 0 {
		t.Fatal("no /api/customizer/* routes registered")
	}
	prefixes := plugin.NoCachePathPrefixes(app.PublicRoutes())
	for _, prefix := range prefixes {
		if prefix == "/api" || prefix == "/api/customizer" {
			t.Fatalf("NoCachePathPrefixes = %#v, refuse %s", prefixes, prefix)
		}
	}
	mw := shared.CacheControlMiddleware(prefixes)
	mux := http.NewServeMux()
	for _, r := range customizer {
		mux.Handle(r.Pattern, r.Handler)
	}
	h := mw(mux)
	for _, r := range customizer {
		path := concreteCustomizerPath(r.Pattern)
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("%s %s (from %s) Cache-Control = %q, want no-store (prefixes %#v)", method, path, r.Pattern, got, prefixes)
			}
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/api/customizer", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s /api/customizer status = %d, want plain 404", method, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got == "no-store" {
			t.Fatalf("%s /api/customizer Cache-Control = %q, bare path may be cached", method, got)
		}
		if method == http.MethodGet && strings.Contains(rec.Body.String(), `"error"`) {
			t.Fatalf("GET /api/customizer body = %q, want plain 404", rec.Body.String())
		}
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/products", nil))
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Fatalf("GET /api/v1/products Cache-Control = %q, want public max-age", got)
	}
}

func concreteCustomizerPath(pattern string) string {
	path := pattern
	if _, rest, ok := strings.Cut(pattern, " "); ok {
		path = rest
	}
	for {
		start := strings.Index(path, "{")
		if start < 0 {
			return path
		}
		end := strings.Index(path[start:], "}")
		if end < 0 {
			return path
		}
		path = path[:start] + "x" + path[start+end+1:]
	}
}

func TestMetalDogs_ServeAndWorkerBootWithoutCatalogSeed(t *testing.T) {
	workerReg, workerApp, workerBoot := metalDogsBootFixture()
	if err := bootWorkerPlugins(workerReg, workerApp, workerBoot); err != nil {
		t.Fatalf("worker boot: %v", err)
	}
	assertMetalDogsBootsWithoutCatalogSeed(t, workerReg, workerApp)

	serveReg, serveApp, serveBoot := metalDogsBootFixture()
	if err := bootServePlugins(serveReg, serveApp, serveBoot, struct{}{}); err != nil {
		t.Fatalf("serve boot: %v", err)
	}
	assertMetalDogsBootsWithoutCatalogSeed(t, serveReg, serveApp)
}

func metalDogsBootFixture() (*plugin.Registry, *plugin.App, *plugin.Bootstrap) {
	cfg := metalDogsEnabledConfig()
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(metaldogs.New())
	app := &plugin.App{Logger: log, Bus: event.NewBus(log), Config: cfg}
	boot := &plugin.Bootstrap{
		Products: seedBootProducts{},
		Variants: seedBootVariants{},
	}
	return reg, app, boot
}

func assertMetalDogsBootsWithoutCatalogSeed(t *testing.T, reg *plugin.Registry, app *plugin.App) {
	t.Helper()
	if !app.CapabilityDeclaredBy(plugin.CapCatalogSeed, metaldogs.PluginName) {
		t.Fatal("metaldogs must Need CapCatalogSeed")
	}
	cmd, ok := reg.CLIRegistry().Get(metaldogs.CommandCatalogSeed)
	if !ok || !cmd.RequiresDB || cmd.Plugin != metaldogs.PluginName {
		t.Fatalf("catalog-seed cmd = %+v, want RequiresDB owned by metaldogs", cmd)
	}
	if _, bound := app.CatalogSeedPort(); bound {
		t.Fatal("serve and worker must leave CatalogSeedPort unbound")
	}
}

var metalDogsCLISchemaSeq atomic.Uint64

func TestRunPluginCLICommand_CatalogSeedWritesProductAndVariant(t *testing.T) {
	dsn := useIsolatedCatalogSchema(t)
	cfg := metalDogsEnabledConfig()
	log := logger.NewWithWriter(io.Discard, "error")
	deps := defaultPluginCLIDeps()
	deps.OpenDB = func(*config.Config) (*sql.DB, error) {
		return db.Open(dsn)
	}
	ran, err := runPluginCLICommandWithDeps(cfg, log, func() (*pluginCLIRuntime, error) {
		return bootstrapPluginCLIRegistry(cfg, log)
	}, metaldogs.CommandCatalogSeed, []string{"-dir", metalDogsCatalogDir(t)}, deps)
	if err != nil {
		t.Fatalf("runPluginCLICommandWithDeps: %v", err)
	}
	if !ran {
		t.Fatal("metaldogs:catalog-seed was not found")
	}

	dbConn := openDatabaseURL(t, dsn)
	defer dbConn.Close()
	var products, variants int
	if err := dbConn.QueryRow(`SELECT count(*) FROM products WHERE slug = 'mdogs-tee'`).Scan(&products); err != nil {
		t.Fatal(err)
	}
	if err := dbConn.QueryRow(`SELECT count(*) FROM variants WHERE sku = 'MDOGS-TEE-S'`).Scan(&variants); err != nil {
		t.Fatal(err)
	}
	if products != 1 || variants != 1 {
		t.Fatalf("products=%d variants=%d, want 1 and 1", products, variants)
	}
}

func metalDogsEnabledConfig() *config.Config {
	return &config.Config{
		Plugins: config.PluginsConfig{
			Extra: map[string]map[string]string{
				"metaldogs": {"enabled": "true"},
			},
		},
	}
}

func metalDogsCatalogDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate register_plugins_metaldogs_test.go")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "plugins", "metaldogs", "catalog")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("catalog dir %s: %v", dir, err)
	}
	return dir
}

func useIsolatedCatalogSchema(t *testing.T) string {
	t.Helper()
	base := os.Getenv("SHOPANDA_TEST_DSN")
	if base == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}
	schema := fmt.Sprintf("md_cliseed_%d_%d", os.Getpid(), metalDogsCLISchemaSeq.Add(1))
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.Ping(); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	})
	isolated := dsnWithSearchPath(t, base, schema)
	mig := openDatabaseURL(t, isolated)
	defer mig.Close()
	var current string
	if err := mig.QueryRow(`SELECT current_schema()`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != schema {
		t.Fatalf("current_schema=%q want %q", current, schema)
	}
	if _, err := migrate.Run(mig, coreMigrationsDir(t)); err != nil {
		t.Fatalf("core migrations: %v", err)
	}
	return isolated
}

func openDatabaseURL(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	return conn
}

func dsnWithSearchPath(t *testing.T, base, schema string) string {
	t.Helper()
	if strings.Contains(base, "://") {
		u, err := url.Parse(base)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String()
	}
	return strings.TrimSpace(base) + " search_path=" + schema
}

func coreMigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate register_plugins_metaldogs_test.go")
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "migrations"))
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		t.Fatalf("migrations dir %s: %v", dir, err)
	}
	return dir
}

func TestAuthMiddleware_CustomizerOpaqueBearerIsGuest(t *testing.T) {
	parser := &opaqueBearerParser{err: fmt.Errorf("%w: opaque", auth.ErrNotCustomerToken)}
	mw := storefront.AuthMiddleware(parser)
	for _, path := range []string{
		"/api/customizer/designs/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
		"/api/customizer",
		"/api/customizer-other",
	} {
		t.Run(path, func(t *testing.T) {
			parser.lastToken = ""
			var gotID identity.Identity
			handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotID = auth.IdentityFrom(r.Context())
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "bearer "+opaqueOwnerToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if !gotID.IsGuest() {
				t.Fatalf("expected guest, got %+v", gotID)
			}
			if parser.lastToken != opaqueOwnerToken {
				t.Fatalf("parser token = %q", parser.lastToken)
			}
		})
	}
}

type opaqueBearerParser struct {
	err       error
	lastToken string
}

func (s *opaqueBearerParser) Parse(_ context.Context, token string) (identity.Identity, error) {
	s.lastToken = token
	return identity.Identity{}, s.err
}

const opaqueOwnerToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
