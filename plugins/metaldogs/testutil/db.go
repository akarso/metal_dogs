package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/plugins/metaldogs"
)

var (
	schemaSafe = regexp.MustCompile(`[^a-z0-9_]+`)
	schemaSeq  atomic.Uint64
)

// OpenIsolated opens SHOPANDA_TEST_DSN, creates a unique schema for this call,
// returns a *sql.DB whose every pool connection has search_path pinned to that
// schema (via libpq options=-csearch_path=…), applies Metal Dogs plugin
// migrations into it, and drops the schema on cleanup.
//
// Skip when SHOPANDA_TEST_DSN is unset. Each call gets a distinct schema name
// (slug + pid + counter), so multiple OpenIsolated calls with the same slug
// are safe. Plugin migrate advisory locks remain process/DB-global per plugin
// name — CI still serializes packages (-p 1) for suites that share a DSN.
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

	bootstrap, err := sql.Open("pgx", dsn)
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

	isolatedDSN, err := dsnWithSearchPath(dsn, schema)
	if err != nil {
		dropSchemaBestEffort(dsn, schema)
		t.Fatalf("dsn with search_path: %v", err)
	}
	db, err := sql.Open("pgx", isolatedDSN)
	if err != nil {
		dropSchemaBestEffort(dsn, schema)
		t.Fatalf("open isolated db: %v", err)
	}
	// Pool is allowed; search_path is a startup GUC on every new connection.
	db.SetMaxOpenConns(8)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		dropSchemaBestEffort(dsn, schema)
		t.Fatalf("ping isolated db: %v", err)
	}

	if _, err := migrate.RunPluginFS(context.Background(), db, metaldogs.PluginName, metaldogs.MigrationFS(), metaldogs.MigrationDir); err != nil {
		_ = db.Close()
		dropSchemaBestEffort(dsn, schema)
		t.Fatalf("metaldogs migrations: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
		dropSchemaBestEffort(dsn, schema)
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

func dsnWithSearchPath(dsn, schema string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	q := u.Query()
	opt := strings.TrimSpace(q.Get("options"))
	sp := "-csearch_path=" + schema
	if opt == "" {
		opt = sp
	} else {
		opt = opt + " " + sp
	}
	q.Set("options", opt)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func dropSchemaBestEffort(dsn, schema string) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.Exec(`DROP SCHEMA IF EXISTS ` + quoteIdent(schema) + ` CASCADE`)
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
