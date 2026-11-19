package main

import (
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs"
)

// registerProductPlugins is the product-owned registration hook.
// Product forks register their plugins here (and only here) so
// register_plugins.go stays an upstream-only compile-time list of
// in-tree demos and reference plugins.
//
// Seeded empty by scripts/product-init.sh when missing. Do not move
// extref / *demo registration into this file upstream.
func registerProductPlugins(registry *plugin.Registry, cfg *config.Config) {
	on, err := metaldogs.Enabled(metalDogsExtra(cfg))
	if err != nil || on {
		registerMetalDogs(registry)
	}
}

func metalDogsExtra(cfg *config.Config) map[string]string {
	if cfg == nil {
		return nil
	}
	return cfg.Plugins.Extra["metaldogs"]
}

func registerMetalDogs(registry *plugin.Registry) {
	registry.Register(metaldogs.New())
}
