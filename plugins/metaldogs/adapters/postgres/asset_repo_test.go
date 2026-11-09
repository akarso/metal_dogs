package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestAssetRepo_InsertGetByID_RoundTrip(t *testing.T) {
	db := testutil.OpenIsolated(t, "assets_repo")
	repo := postgres.NewAssetRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	a := domain.Asset{
		ID:          "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
		Type:        domain.AssetTypeSource,
		StorageKey:  "private/blobs/metaldogs/source/2026/11/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee.jpg",
		MimeType:    "image/jpeg",
		SizeBytes:   1234,
		Width:       512,
		Height:      512,
		Checksum:    application.ChecksumSHA256Prefix + "deadbeef",
		CreatedAt:   now,
		DeleteAfter: now.Add(2 * time.Hour),
	}
	if err := repo.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, ok, err := repo.GetByID(ctx, a.ID)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.StorageKey != a.StorageKey || got.MimeType != a.MimeType || got.Width != 512 || got.Checksum != a.Checksum {
		t.Fatalf("got=%+v", got)
	}
	if got.OwnerRef != "" {
		t.Fatalf("owner_ref=%q want empty", got.OwnerRef)
	}

	// UNIQUE(storage_key)
	dup := a
	dup.ID = "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	if err := repo.Insert(ctx, dup); err == nil {
		t.Fatal("duplicate storage_key must fail")
	}

	if err := repo.SoftDelete(ctx, a.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, ok, err = repo.GetByID(ctx, a.ID)
	if err != nil || ok {
		t.Fatalf("soft-deleted must be hidden: ok=%v err=%v", ok, err)
	}
}
