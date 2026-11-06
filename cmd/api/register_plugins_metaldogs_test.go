package main

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
	if err := p.Init(app); err != nil {
		t.Fatalf("Init registered plugin: %v", err)
	}
	routes := app.PublicRoutes()
	if len(routes) != 1 || routes[0].Pattern != "GET /api/customizer/enabled" {
		t.Fatalf("PublicRoutes = %+v, want GET /api/customizer/enabled", routes)
	}
	rec := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/enabled", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
