package metaldogs

import (
	"context"
	"database/sql"
	"embed"

	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var _ plugin.Migrator = (*Plugin)(nil)

// Migration implements plugin.Migrator for app migrate / InitAll.
// Versions are recorded as plugin:metaldogs:<basename>.
func (p *Plugin) Migration() plugin.MigrationSpec {
	return plugin.MigrationSpec{
		FS:  migrationFiles,
		Dir: "migrations",
	}
}

// RunMigrationsForTest applies embedded Metal Dogs migrations (integration tests only).
func RunMigrationsForTest(db *sql.DB) (migrate.PluginFSResult, error) {
	return migrate.RunPluginFS(context.Background(), db, PluginName, migrationFiles, "migrations")
}
