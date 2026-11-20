package testutil

import (
	"database/sql"
	"testing"

	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/pkg/pluginsdk/harness"
	"github.com/akarso/shopanda/plugins/metaldogs"
)

// OpenIsolated opens SHOPANDA_TEST_DSN into a fresh schema via harness.OpenIsolated,
// applies Metal Dogs plugin migrations, and drops the schema on cleanup.
// Skips when the DSN is unset. packageSlug is unused for naming; the advisory
// lock is still per plugin name, so CI keeps -p 1.
func OpenIsolated(t *testing.T, packageSlug string) *sql.DB {
	t.Helper()
	return openIsolated(t, packageSlug, false)
}

// OpenIsolatedWithCore is OpenIsolated plus Shopanda core migrations.
// Skip when SHOPANDA_TEST_DSN is unset, same as OpenIsolated.
func OpenIsolatedWithCore(t *testing.T, packageSlug string) *sql.DB {
	t.Helper()
	return openIsolated(t, packageSlug, true)
}

func openIsolated(t *testing.T, _ string, core bool) *sql.DB {
	t.Helper()
	coreDir := ""
	if !core {
		coreDir = "-"
	}
	iso := harness.OpenIsolated(t, harness.IsolatedOptions{
		CoreMigrationsDir: coreDir,
		PluginName:        metaldogs.PluginName,
		PluginMigration: pluginsdk.MigrationSpec{
			FS:  metaldogs.MigrationFS(),
			Dir: metaldogs.MigrationDir,
		},
	})
	return iso.DB
}
