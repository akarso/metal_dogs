package metaldogs

import (
	"embed"
	"io/fs"

	"github.com/akarso/shopanda/pkg/pluginsdk"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var _ pluginsdk.Migrator = (*Plugin)(nil)

// Migration implements pluginsdk.Migrator for app migrate / InitAll.
// Versions are recorded as plugin:metaldogs:<basename>.
func (p *Plugin) Migration() pluginsdk.MigrationSpec {
	return pluginsdk.MigrationSpec{
		FS:  migrationFiles,
		Dir: "migrations",
	}
}

// MigrationFS returns the embedded SQL migrations for the test harness.
// Tests apply them through testutil.OpenIsolated (harness.OpenIsolated).
func MigrationFS() fs.FS { return migrationFiles }

// MigrationDir is the directory inside MigrationFS() that holds *.sql files.
const MigrationDir = "migrations"
