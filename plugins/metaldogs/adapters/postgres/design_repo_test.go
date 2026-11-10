package postgres_test

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestDesignRepo_CreateBound_AndAuthRoundTrip(t *testing.T) {
	db := testutil.OpenIsolated(t, "designs_repo")
	assets := postgres.NewAssetRepo(db)
	designs := postgres.NewDesignRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	assetID := "aaaaaaaa-bbbb-4ccc-8ddd-aaaaaaaaaaaa"
	designID := "bbbbbbbb-bbbb-4ccc-8ddd-bbbbbbbbbbbb"
	a := domain.Asset{
		ID: assetID, Type: domain.AssetTypeSource,
		StorageKey: "private/blobs/metaldogs/source/2026/11/" + assetID + ".jpg",
		MimeType:   "image/jpeg", SizeBytes: 1, Width: 300, Height: 300,
		Checksum: application.ChecksumSHA256Prefix + "ab", CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
	}
	if err := assets.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256([]byte("not-a-real-token-bytes-32!!!!!!!!"))
	d := domain.Design{
		ID: designID, TemplateID: "cccccccc-cccc-4ccc-8ddd-cccccccccccc", TemplateVersion: 1,
		Status: domain.DesignDraft, Texts: map[string]string{"dog_name": "Burek"},
		SourceAssetID: assetID, OwnerTokenHash: sum[:],
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(domain.DesignRetentionUnordered),
	}
	if err := designs.CreateBound(ctx, d); err != nil {
		t.Fatal(err)
	}
	gotAsset, ok, err := assets.GetByID(ctx, assetID)
	if err != nil || !ok || gotAsset.OwnerRef != designID {
		t.Fatalf("owner_ref=%q ok=%v err=%v", gotAsset.OwnerRef, ok, err)
	}
	got, ok, err := designs.GetByID(ctx, designID)
	if err != nil || !ok || got.Texts["dog_name"] != "Burek" || len(got.OwnerTokenHash) != 32 {
		t.Fatalf("%+v ok=%v err=%v", got, ok, err)
	}

	// Second bind must conflict.
	if err := assets.BindOwnerRef(ctx, assetID, "other"); err != ports.ErrAssetBindConflict {
		t.Fatalf("err=%v", err)
	}

	// Soft-deleted upload cannot bind.
	asset2 := a
	asset2.ID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	asset2.StorageKey = "private/blobs/metaldogs/source/2026/11/" + asset2.ID + ".jpg"
	if err := assets.Insert(ctx, asset2); err != nil {
		t.Fatal(err)
	}
	if err := assets.SoftDelete(ctx, asset2.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := assets.BindOwnerRef(ctx, asset2.ID, designID); err != ports.ErrAssetBindConflict {
		t.Fatalf("soft-deleted bind err=%v", err)
	}
}
