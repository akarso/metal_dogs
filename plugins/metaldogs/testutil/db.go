package testutil

import (
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/plugins/metaldogs"
)

var schemaSafe = regexp.MustCompile(`[^a-z0-9_]+`)

// OpenIsolated opens SHOPANDA_TEST_DSN, creates a dedicated schema for this
// test package slug, sets search_path to that schema only, applies Metal Dogs
// plugin migrations into it, and drops the schema on cleanup.
//
// Skip when SHOPANDA_TEST_DSN is unset. Safe for parallel packages when each
// uses a distinct slug; CI still serializes packages (-p 1) for core suites.
func OpenIsolated(t *testing.T, packageSlug string) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set; skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatalf("ping db: %v", err)
	}

	slug := schemaSafe.ReplaceAllString(strings.ToLower(packageSlug), "_")
	if slug == "" {
		slug = "pkg"
	}
	if len(slug) > 40 {
		slug = slug[:40]
	}
	schema := fmt.Sprintf("md_%s_%d", slug, os.Getpid())

	if _, err := db.Exec(`CREATE SCHEMA ` + quoteIdent(schema)); err != nil {
		_ = db.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}
	if _, err := db.Exec(`SET search_path TO ` + quoteIdent(schema)); err != nil {
		_, _ = db.Exec(`DROP SCHEMA ` + quoteIdent(schema) + ` CASCADE`)
		_ = db.Close()
		t.Fatalf("set search_path: %v", err)
	}

	if _, err := metaldogs.RunMigrationsForTest(db); err != nil {
		_, _ = db.Exec(`DROP SCHEMA ` + quoteIdent(schema) + ` CASCADE`)
		_ = db.Close()
		t.Fatalf("metaldogs migrations: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`SET search_path TO public`)
		_, _ = db.Exec(`DROP SCHEMA IF EXISTS ` + quoteIdent(schema) + ` CASCADE`)
		_ = db.Close()
	})
	return db
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
