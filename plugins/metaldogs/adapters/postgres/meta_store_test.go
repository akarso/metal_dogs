package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestMetaStore_RoundTrip(t *testing.T) {
	db := testutil.OpenIsolated(t, "adapters_postgres")
	store := postgres.NewMetaStore(db)
	ctx := context.Background()
	ids := testutil.NewSeqGenerator("meta")
	key := ids.New()

	if err := store.Put(ctx, key, "hello"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || got != "hello" {
		t.Fatalf("Get = %q ok=%v, want hello true", got, ok)
	}

	if err := store.Put(ctx, key, "updated"); err != nil {
		t.Fatalf("Put update: %v", err)
	}
	got, ok, err = store.Get(ctx, key)
	if err != nil || !ok || got != "updated" {
		t.Fatalf("Get after update = %q ok=%v err=%v", got, ok, err)
	}

	_, ok, err = store.Get(ctx, ids.New())
	if err != nil || ok {
		t.Fatalf("missing key: ok=%v err=%v", ok, err)
	}
}

func TestMetaStore_PutEmptyKey(t *testing.T) {
	db := testutil.OpenIsolated(t, "adapters_postgres_empty")
	store := postgres.NewMetaStore(db)
	err := store.Put(context.Background(), "", "x")
	if err == nil || !strings.Contains(err.Error(), "key required") {
		t.Fatalf("Put empty key: %v", err)
	}
}
