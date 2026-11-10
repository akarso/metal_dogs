package postgres_test

import (
	"context"
	"crypto/sha256"
	"strings"
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
	attachedAfter := now.Add(domain.SourceAttachedRetention)
	if err := designs.CreateBound(ctx, d, attachedAfter); err != nil {
		t.Fatal(err)
	}
	gotAsset, ok, err := assets.GetByID(ctx, assetID)
	if err != nil || !ok || gotAsset.OwnerRef != designID {
		t.Fatalf("owner_ref=%q ok=%v err=%v", gotAsset.OwnerRef, ok, err)
	}
	if !gotAsset.DeleteAfter.Equal(attachedAfter) {
		t.Fatalf("delete_after=%v want %v", gotAsset.DeleteAfter, attachedAfter)
	}
	got, ok, err := designs.GetByID(ctx, designID)
	if err != nil || !ok || got.Texts["dog_name"] != "Burek" || len(got.OwnerTokenHash) != 32 {
		t.Fatalf("%+v ok=%v err=%v", got, ok, err)
	}

	// Second bind must conflict.
	if err := assets.BindOwnerRef(ctx, assetID, "other", attachedAfter); err != ports.ErrAssetBindConflict {
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
	if err := assets.BindOwnerRef(ctx, asset2.ID, designID, attachedAfter); err != ports.ErrAssetBindConflict {
		t.Fatalf("soft-deleted bind err=%v", err)
	}
}

func TestDesignRepo_ReplaceSource_ConcurrentExclusive(t *testing.T) {
	db := testutil.OpenIsolated(t, "designs_replace")
	assets := postgres.NewAssetRepo(db)
	designs := postgres.NewDesignRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	oldID := "aaaaaaaa-bbbb-4ccc-8ddd-111111111111"
	newA := "aaaaaaaa-bbbb-4ccc-8ddd-222222222222"
	newB := "aaaaaaaa-bbbb-4ccc-8ddd-333333333333"
	designID := "bbbbbbbb-bbbb-4ccc-8ddd-444444444444"
	for _, id := range []string{oldID, newA, newB} {
		a := domain.Asset{
			ID: id, Type: domain.AssetTypeSource,
			StorageKey: "private/blobs/metaldogs/source/2026/11/" + id + ".jpg",
			MimeType:   "image/jpeg", SizeBytes: 1, Width: 300, Height: 300,
			Checksum: application.ChecksumSHA256Prefix + id[:8], CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
		}
		if err := assets.Insert(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256([]byte("concurrent-replace-token-bytes-32!"))
	d := domain.Design{
		ID: designID, TemplateID: "cccccccc-cccc-4ccc-8ddd-cccccccccccc", TemplateVersion: 1,
		Status: domain.DesignDraft, Texts: map[string]string{"dog_name": "Burek"},
		SourceAssetID: oldID, OwnerTokenHash: sum[:],
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(domain.DesignRetentionUnordered),
	}
	if err := designs.CreateBound(ctx, d, now.Add(domain.SourceAttachedRetention)); err != nil {
		t.Fatal(err)
	}

	type result struct {
		which string
		key   string
		err   error
	}
	ch := make(chan result, 2)
	run := func(which, newID string) {
		nd := d
		nd.SourceAssetID = newID
		nd.UpdatedAt = now.Add(time.Minute)
		nd.ExpiresAt = now.Add(domain.DesignRetentionUnordered)
		key, err := designs.ReplaceSource(ctx, nd, oldID, now, now.Add(domain.SourceAttachedRetention))
		ch <- result{which: which, key: key, err: err}
	}
	go run("A", newA)
	go run("B", newB)
	r1, r2 := <-ch, <-ch
	okCount := 0
	var winnerKey string
	for _, r := range []result{r1, r2} {
		if r.err == nil {
			okCount++
			winnerKey = r.key
			continue
		}
		if r.err != ports.ErrDesignConflict && r.err != ports.ErrAssetBindConflict {
			t.Fatalf("%s unexpected err=%v", r.which, r.err)
		}
	}
	if okCount != 1 {
		t.Fatalf("want exactly one winner, got ok=%d r1=%v r2=%v", okCount, r1.err, r2.err)
	}
	if winnerKey == "" || !strings.Contains(winnerKey, oldID) {
		t.Fatalf("winner must return old storage key, got %q", winnerKey)
	}
	got, ok, err := designs.GetByID(ctx, designID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if got.SourceAssetID != newA && got.SourceAssetID != newB {
		t.Fatalf("source=%q", got.SourceAssetID)
	}
	winnerAsset, ok, err := assets.GetByID(ctx, got.SourceAssetID)
	if err != nil || !ok || winnerAsset.OwnerRef != designID {
		t.Fatalf("winner owner_ref=%q ok=%v err=%v", winnerAsset.OwnerRef, ok, err)
	}
	old, ok, err := assets.GetByID(ctx, oldID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("old source must be soft-deleted, still visible: %+v", old)
	}
	// Loser upload must remain unbound so it can still be attached later.
	loser := newA
	if got.SourceAssetID == newA {
		loser = newB
	}
	la, ok, err := assets.GetByID(ctx, loser)
	if err != nil || !ok || la.OwnerRef != "" {
		t.Fatalf("loser asset %+v ok=%v err=%v", la, ok, err)
	}
}
