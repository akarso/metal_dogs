package main

import (
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/example"
	"github.com/akarso/shopanda/plugins/metaldogs"
)

var cliSeedSchemaSeq atomic.Uint64

func TestRunPluginCLICommand_UnknownReturnsFalse(t *testing.T) {
	cfg := &config.Config{}
	log := logger.New("error")
	ran, err := runPluginCLICommand(cfg, log, func() (*plugin.Registry, *plugin.App, error) {
		return bootstrapPluginCLIRegistry(cfg, log)
	}, "does-not-exist", nil)
	if err != nil {
		t.Fatalf("runPluginCLICommand: %v", err)
	}
	if ran {
		t.Fatal("expected ran=false for unknown command")
	}
}

func TestPluginCLIHelpLines_IncludesExampleWhenEnabled(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: true},
		},
	}
	log := logger.New("error")
	registryFn := func() (*plugin.Registry, *plugin.App, error) {
		return bootstrapPluginCLIRegistry(cfg, log)
	}
	lines := pluginCLIHelpLines(registryFn)
	if len(lines) < 2 {
		t.Fatalf("help lines = %v, want plugin section", lines)
	}
	found := false
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == example.CommandPing && strings.Contains(line, "Verify example plugin CLI registration") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("help lines = %v, want example:ping entry", lines)
	}
}

func TestRunPluginCLICommand_CatalogSeedWritesProductAndVariant(t *testing.T) {
	useIsolatedCatalogSchema(t, true)
	cfg := metalDogsCLIConfig()
	log := logger.NewWithWriter(io.Discard, "error")
	ran, err := runPluginCLICommand(cfg, log, func() (*plugin.Registry, *plugin.App, error) {
		return bootstrapPluginCLIRegistry(cfg, log)
	}, metaldogs.CommandCatalogSeed, []string{"-dir", metalDogsCatalogDir(t)})
	if err != nil {
		t.Fatalf("runPluginCLICommand: %v", err)
	}
	if !ran {
		t.Fatal("metaldogs:catalog-seed was not found")
	}

	db := openDatabaseURL(t)
	defer db.Close()
	var products, variants int
	if err := db.QueryRow(`SELECT count(*) FROM products WHERE slug = 'mdogs-tee'`).Scan(&products); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM variants WHERE sku = 'MDOGS-TEE-S'`).Scan(&variants); err != nil {
		t.Fatal(err)
	}
	if products != 1 || variants != 1 {
		t.Fatalf("products=%d variants=%d, want 1 and 1", products, variants)
	}
}

func TestRunPluginCLICommand_CatalogSeedGetWithoutUnboundPort(t *testing.T) {
	useIsolatedCatalogSchema(t, false)
	cfg := metalDogsCLIConfig()
	log := logger.NewWithWriter(io.Discard, "error")
	dir := t.TempDir()
	ran, err := runPluginCLICommand(cfg, log, func() (*plugin.Registry, *plugin.App, error) {
		return bootstrapPluginCLIRegistry(cfg, log)
	}, metaldogs.CommandCatalogSeed, []string{"-dir", dir})
	if !ran {
		t.Fatal("metaldogs:catalog-seed was not found")
	}
	if err == nil {
		t.Fatal("empty catalog dir must fail")
	}
	if strings.Contains(err.Error(), "catalog seed port is not configured") {
		t.Fatalf("port unbound: %v", err)
	}
}

func metalDogsCLIConfig() *config.Config {
	return &config.Config{
		Plugins: config.PluginsConfig{
			Extra: map[string]map[string]string{
				"metaldogs": {"enabled": "true"},
			},
		},
	}
}

func metalDogsCatalogDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate cli_plugins_test.go")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "plugins", "metaldogs", "catalog")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("catalog dir %s: %v", dir, err)
	}
	return dir
}

func useIsolatedCatalogSchema(t *testing.T, migrateCore bool) {
	t.Helper()
	base := os.Getenv("SHOPANDA_TEST_DSN")
	if base == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}
	schema := fmt.Sprintf("md_cliseed_%d_%d", os.Getpid(), cliSeedSchemaSeq.Add(1))
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.Ping(); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	})
	isolated := dsnWithSearchPath(t, base, schema)
	t.Setenv("DATABASE_URL", isolated)
	if !migrateCore {
		return
	}
	db := openDatabaseURL(t)
	defer db.Close()
	var current string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != schema {
		t.Fatalf("current_schema=%q want %q", current, schema)
	}
	if _, err := migrate.Run(db, coreMigrationsDir(t)); err != nil {
		t.Fatalf("core migrations: %v", err)
	}
}

func openDatabaseURL(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	return db
}

func dsnWithSearchPath(t *testing.T, base, schema string) string {
	t.Helper()
	if strings.Contains(base, "://") {
		u, err := url.Parse(base)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String()
	}
	return strings.TrimSpace(base) + " search_path=" + schema
}

func coreMigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate cli_plugins_test.go")
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "migrations"))
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		t.Fatalf("migrations dir %s: %v", dir, err)
	}
	return dir
}
