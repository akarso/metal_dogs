package testutil_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestOpenIsolated_PoolSeesSchema(t *testing.T) {
	db := testutil.OpenIsolated(t, "pool_sees")
	db.SetMaxOpenConns(8)

	const workers = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := context.Background()
			var sp string
			if err := db.QueryRowContext(ctx, `SHOW search_path`).Scan(&sp); err != nil {
				errs <- err
				return
			}
			if !strings.Contains(sp, "md_") || strings.Contains(sp, "public") {
				errs <- fmt.Errorf("search_path=%q want isolated md_* without public", sp)
				return
			}
			key := fmt.Sprintf("k-%d", i)
			if _, err := db.ExecContext(ctx, `
				INSERT INTO metaldogs_meta (key, value) VALUES ($1, $2)
				ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
			`, key, "v"); err != nil {
				errs <- err
				return
			}
			var n int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM metaldogs_meta`).Scan(&n); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("pool connection missed isolated schema: %v", err)
		}
	}
}

func TestOpenIsolated_SameSlugDistinctSchemas(t *testing.T) {
	db1 := testutil.OpenIsolated(t, "same_slug")
	db2 := testutil.OpenIsolated(t, "same_slug")
	var sp1, sp2 string
	if err := db1.QueryRow(`SHOW search_path`).Scan(&sp1); err != nil {
		t.Fatalf("sp1: %v", err)
	}
	if err := db2.QueryRow(`SHOW search_path`).Scan(&sp2); err != nil {
		t.Fatalf("sp2: %v", err)
	}
	if sp1 == sp2 {
		t.Fatalf("expected distinct schemas, both %q", sp1)
	}
	if _, err := db1.Exec(`INSERT INTO metaldogs_meta (key, value) VALUES ('a', '1')`); err != nil {
		t.Fatalf("insert db1: %v", err)
	}
	var n int
	if err := db2.QueryRow(`SELECT count(*) FROM metaldogs_meta`).Scan(&n); err != nil {
		t.Fatalf("count db2: %v", err)
	}
	if n != 0 {
		t.Fatalf("db2 saw db1 rows (n=%d); schemas are not isolated", n)
	}
}

func TestOpenIsolated_CleanupDropsSchema(t *testing.T) {
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set; skipping integration test")
	}

	var schema string
	t.Run("inner", func(t *testing.T) {
		db := testutil.OpenIsolated(t, "cleanup")
		if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
			t.Fatalf("current_schema: %v", err)
		}
		if schema == "" || schema == "public" {
			t.Fatalf("unexpected schema %q", schema)
		}
	})

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer admin.Close()
	var exists bool
	err = admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = $1)`, schema).Scan(&exists)
	if err != nil {
		t.Fatalf("pg_namespace: %v", err)
	}
	if exists {
		t.Fatalf("schema %q still exists after OpenIsolated cleanup", schema)
	}
}
