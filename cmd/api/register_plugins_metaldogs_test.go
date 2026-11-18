package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
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
	if !patterns["GET /api/customizer"] || !patterns["GET /api/customizer/enabled"] || !patterns["GET /api/customizer/health"] ||
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

	prefixes := plugin.NoCachePathPrefixes(app.PublicRoutes())
	hasCustomizer := false
	for _, prefix := range prefixes {
		if prefix == "/api" {
			t.Fatalf("NoCachePathPrefixes = %#v, refuse /api", prefixes)
		}
		if prefix == "/api/customizer" {
			hasCustomizer = true
		}
	}
	if !hasCustomizer {
		t.Fatalf("NoCachePathPrefixes = %#v, want /api/customizer", prefixes)
	}
	mw := shared.CacheControlMiddleware(prefixes)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, path := range []string{"/api/customizer", "/api/customizer/enabled", "/api/customizer/designs/design-1"} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("%s %s Cache-Control = %q, want no-store (prefixes %#v)", method, path, got, prefixes)
			}
		}
	}
	var root plugin.PublicRoute
	for _, r := range routes {
		if r.Pattern == "GET /api/customizer" {
			root = r
		}
	}
	rootMux := http.NewServeMux()
	rootMux.Handle(root.Pattern, root.Handler)
	rootH := mw(rootMux)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		rootH.ServeHTTP(rec, httptest.NewRequest(method, "/api/customizer", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s /api/customizer status = %d, want 404", method, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s /api/customizer Cache-Control = %q, want no-store", method, got)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("%s /api/customizer Content-Type = %q, want application/json", method, ct)
		}
		if method == http.MethodGet && !strings.Contains(rec.Body.String(), `"error":"not_found"`) {
			t.Fatalf("GET /api/customizer body = %q, want JSON not_found", rec.Body.String())
		}
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/products", nil))
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Fatalf("GET /api/v1/products Cache-Control = %q, want public max-age", got)
	}
}
