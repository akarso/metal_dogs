package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/plugins/metaldogs"
)

var (
	schemaSafe = regexp.MustCompile(`[^a-z0-9_]+`)
	schemaSeq  atomic.Uint64
)

// OpenIsolated opens SHOPANDA_TEST_DSN, creates a unique schema for this call,
// returns a *sql.DB whose every pool connection has search_path pinned to that
// schema (via pgx RuntimeParams), applies Metal Dogs plugin migrations into it,
// and drops the schema on cleanup.
//
// Skip when SHOPANDA_TEST_DSN is unset. Each call gets a distinct schema name
// (slug + pid + counter), so multiple OpenIsolated calls with the same slug
// are safe. Plugin migrate advisory locks remain process/DB-global per plugin
// name — CI still serializes packages (-p 1) for suites that share a DSN.
//
// DSN may be a postgres(ql):// URL or a libpq keyword/value string; both are
// accepted via pgx.ParseConfig.
func OpenIsolated(t *testing.T, packageSlug string) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set; skipping integration test")
	}

	slug := schemaSafe.ReplaceAllString(strings.ToLower(packageSlug), "_")
	if slug == "" {
		slug = "pkg"
	}
	schema := uniqueSchemaName(slug)

	bootstrap, err := openDB(dsn)
	if err != nil {
		t.Fatalf("open bootstrap db: %v", err)
	}
	if err := bootstrap.Ping(); err != nil {
		_ = bootstrap.Close()
		t.Fatalf("ping bootstrap db: %v", err)
	}
	if _, err := bootstrap.Exec(`CREATE SCHEMA ` + quoteIdent(schema)); err != nil {
		_ = bootstrap.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}
	_ = bootstrap.Close()

	db, err := openDBWithSearchPath(dsn, schema)
	if err != nil {
		dropSchemaBestEffort(t, dsn, schema)
		t.Fatalf("open isolated db: %v", err)
	}
	// Pool is allowed; search_path is a startup GUC on every new connection.
	db.SetMaxOpenConns(8)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		dropSchemaBestEffort(t, dsn, schema)
		t.Fatalf("ping isolated db: %v", err)
	}

	if _, err := migrate.RunPluginFS(context.Background(), db, metaldogs.PluginName, metaldogs.MigrationFS(), metaldogs.MigrationDir); err != nil {
		_ = db.Close()
		dropSchemaBestEffort(t, dsn, schema)
		t.Fatalf("metaldogs migrations: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
		dropSchemaBestEffort(t, dsn, schema)
	})
	return db
}

func uniqueSchemaName(slug string) string {
	n := schemaSeq.Add(1)
	// Keep under PostgreSQL's 63-byte identifier limit.
	const maxLen = 63
	base := fmt.Sprintf("md_%s_%d_%d", slug, os.Getpid(), n)
	if len(base) <= maxLen {
		return base
	}
	over := len(base) - maxLen
	if over >= len(slug) {
		slug = "x"
	} else {
		slug = slug[:len(slug)-over]
	}
	return fmt.Sprintf("md_%s_%d_%d", slug, os.Getpid(), n)
}

func openDB(dsn string) (*sql.DB, error) {
	cfg, err := parseDSN(dsn)
	if err != nil {
		return nil, err
	}
	return stdlib.OpenDB(*cfg), nil
}

func openDBWithSearchPath(dsn, schema string) (*sql.DB, error) {
	cfg, err := configWithSearchPath(dsn, schema)
	if err != nil {
		return nil, err
	}
	return stdlib.OpenDB(*cfg), nil
}

// configWithSearchPath parses dsn (URL or libpq keywords) and sets search_path.
func configWithSearchPath(dsn, schema string) (*pgx.ConnConfig, error) {
	if schema == "" {
		return nil, fmt.Errorf("schema required")
	}
	cfg, err := parseDSN(dsn)
	if err != nil {
		return nil, err
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = map[string]string{}
	}
	cfg.RuntimeParams["search_path"] = schema
	return cfg, nil
}

func parseDSN(dsn string) (*pgx.ConnConfig, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("empty DSN")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN (want postgres(ql):// URL or libpq keyword/value string): %w", err)
	}
	return cfg, nil
}

func dropSchemaBestEffort(t testing.TB, dsn, schema string) {
	t.Helper()
	db, err := openDB(dsn)
	if err != nil {
		t.Logf("drop schema %s: open admin db: %v", schema, err)
		return
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS ` + quoteIdent(schema) + ` CASCADE`); err != nil {
		t.Logf("drop schema %s: %v", schema, err)
	}
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
