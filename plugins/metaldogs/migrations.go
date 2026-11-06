package metaldogs

import (
	"embed"
	"io/fs"

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

// MigrationFS returns the embedded SQL migrations for the test harness.
// Prefer plugins/metaldogs/testutil.OpenIsolated over calling migrate directly.
func MigrationFS() fs.FS { return migrationFiles }

// MigrationDir is the directory inside MigrationFS() that holds *.sql files.
const MigrationDir = "migrations"
