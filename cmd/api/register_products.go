package main

import (
	"fmt"

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
	on, err := metaldogs.Enabled(cfg)
	if err != nil || on {
		registerMetalDogs(registry)
	}
}

func registerMetalDogs(registry *plugin.Registry) {
	registry.Register(metaldogs.New())
}

// prepareProductCLI binds ports for product commands that serve and worker skip.
// The command map stays in this file; cli_plugins.go only calls this hook.
func prepareProductCLI(name string, app *plugin.App, openBoot func() (*plugin.Bootstrap, error)) error {
	wire, ok := map[string]func(*plugin.App, *plugin.Bootstrap) error{
		metaldogs.CommandCatalogSeed: wireCatalogSeedCLI,
	}[name]
	if !ok {
		return nil
	}
	if openBoot == nil {
		return fmt.Errorf("catalog seed CLI: plugin bootstrap is not initialized")
	}
	boot, err := openBoot()
	if err != nil {
		return err
	}
	return wire(app, boot)
}

// wireCatalogSeedCLI binds the write port and skips CapCart only, so a missing CapCatalogSeed fails the command.
func wireCatalogSeedCLI(app *plugin.App, boot *plugin.Bootstrap) error {
	if app == nil {
		return fmt.Errorf("catalog seed CLI: plugin app is not initialized")
	}
	if err := bindProductPorts(app, boot); err != nil {
		return err
	}
	if err := bindCatalogSeedPort(app, boot); err != nil {
		return err
	}
	if err := app.AssertNeedsMetExcept(plugin.CapCart); err != nil {
		return fmt.Errorf("plugin capabilities: %w", err)
	}
	return nil
}
