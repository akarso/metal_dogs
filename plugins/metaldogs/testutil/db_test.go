package testutil_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestOpenIsolated_PoolSeesSchema(t *testing.T) {
	db := testutil.OpenIsolated(t, "pool_sees")
	db.SetMaxOpenConns(8)

	var wantSchema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&wantSchema); err != nil {
		t.Fatalf("current_schema: %v", err)
	}
	if wantSchema == "" || wantSchema == "public" {
		t.Fatalf("unexpected schema %q", wantSchema)
	}

	const workers = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	var inserts atomic.Int64
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := context.Background()
			var got string
			if err := db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&got); err != nil {
				errs <- err
				return
			}
			if got != wantSchema {
				errs <- fmt.Errorf("current_schema=%q want %q", got, wantSchema)
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
			inserts.Add(1)
			var n int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM metaldogs_meta`).Scan(&n); err != nil {
				errs <- err
				return
			}
			if n < 1 {
				errs <- fmt.Errorf("count=%d after insert", n)
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
	if inserts.Load() != workers {
		t.Fatalf("inserts=%d want %d", inserts.Load(), workers)
	}
	var total int
	if err := db.QueryRow(`SELECT count(*) FROM metaldogs_meta`).Scan(&total); err != nil {
		t.Fatalf("final count: %v", err)
	}
	if total != workers {
		t.Fatalf("final count=%d want %d (cross-goroutine visibility)", total, workers)
	}
	// Spot-check another connection sees the same rows (pool checkout).
	var total2 int
	if err := db.QueryRow(`SELECT count(*) FROM metaldogs_meta WHERE value = 'v'`).Scan(&total2); err != nil {
		t.Fatalf("recount: %v", err)
	}
	if total2 != workers {
		t.Fatalf("recount=%d want %d", total2, workers)
	}
}

func TestOpenIsolated_SameSlugDistinctSchemas(t *testing.T) {
	db1 := testutil.OpenIsolated(t, "same_slug")
	db2 := testutil.OpenIsolated(t, "same_slug")
	var s1, s2 string
	if err := db1.QueryRow(`SELECT current_schema()`).Scan(&s1); err != nil {
		t.Fatalf("s1: %v", err)
	}
	if err := db2.QueryRow(`SELECT current_schema()`).Scan(&s2); err != nil {
		t.Fatalf("s2: %v", err)
	}
	if s1 == s2 {
		t.Fatalf("expected distinct schemas, both %q", s1)
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
