package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/rbac"
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

func TestRegisterPlugins_MetalDogs_Enabled_RouteAndMigrator(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			MetalDogs: config.MetalDogsPluginConfig{Enabled: true},
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
	if !patterns["GET /api/customizer/enabled"] || !patterns["GET /api/customizer/health"] ||
		!patterns["GET /api/customizer/templates"] || !patterns["GET /api/customizer/templates/{id}"] ||
		!patterns["POST /api/customizer/uploads"] {
		t.Fatalf("PublicRoutes = %+v, want enabled + health + templates + uploads", routes)
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
}
