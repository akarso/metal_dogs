package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// pluginCLIOpen returns the registry and the App passed to InitAll together.
// Callers that bind ports (catalog seed) must use that App, not a later one.
type pluginCLIOpen func() (*plugin.Registry, *plugin.App, error)

func bootstrapPluginCLIRegistry(cfg *config.Config, log logger.Logger) (*plugin.Registry, *plugin.App, error) {
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	app := &plugin.App{
		Logger: log,
		Config: cfg,
	}
	preparePermissionRegistry(app)
	summary := registry.InitAll(app)
	freezePermissionRegistry(app) // CLI: freeze only (no BindRuntime; avoid multi-App collisions)
	if err := plugin.InitSummaryError(summary); err != nil {
		return nil, nil, err
	}
	return registry, app, nil
}

func runPluginCLICommand(cfg *config.Config, log logger.Logger, open pluginCLIOpen, name string, args []string) (bool, error) {
	registry, app, err := open()
	if err != nil {
		return false, err
	}
	cmdRegistry := registry.CLIRegistry()
	if cmdRegistry == nil {
		return false, nil
	}
	cmd, ok := cmdRegistry.Get(name)
	if !ok {
		return false, nil
	}

	ctx := cli.Context{
		Ctx:    context.Background(),
		Config: cfg,
		Logger: log,
	}
	if cmd.RequiresDB {
		dsn := config.DatabaseDSN(cfg)
		conn, err := db.Open(dsn)
		if err != nil {
			return true, fmt.Errorf("database: %w", err)
		}
		defer conn.Close()
		ctx.DB = conn
		if err := prepareProductCLI(name, app, func() (*plugin.Bootstrap, error) {
			return newPluginBootstrap(conn)
		}); err != nil {
			return true, err
		}
	}
	return true, cmdRegistry.Run(name, ctx, args)
}

func pluginCLIHelpLines(open pluginCLIOpen) []string {
	registry, _, err := open()
	if err != nil || registry == nil || registry.CLIRegistry() == nil {
		return nil
	}
	cmds := registry.CLIRegistry().List()
	if len(cmds) == 0 {
		return nil
	}
	lines := make([]string, 0, len(cmds)+1)
	lines = append(lines, "Plugin commands:")
	for _, cmd := range cmds {
		lines = append(lines, fmt.Sprintf("  %-20s %s", cmd.Name, cmd.Description))
	}
	return lines
}

func appendPluginCLIHelp(open pluginCLIOpen, base string) string {
	lines := pluginCLIHelpLines(open)
	if len(lines) == 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	if !strings.HasSuffix(base, "\n") {
		b.WriteByte('\n')
	}
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
