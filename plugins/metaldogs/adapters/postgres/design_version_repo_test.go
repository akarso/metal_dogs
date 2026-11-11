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

func TestDesignVersionRepo_ApproveBound_UniqueAndStatusRace(t *testing.T) {
	db := testutil.OpenIsolated(t, "design_versions")
	assets := postgres.NewAssetRepo(db)
	designs := postgres.NewDesignRepo(db)
	versions := postgres.NewDesignVersionRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	sourceID := "aaaaaaaa-bbbb-4ccc-8ddd-aaaaaaaaaaaa"
	designID := "bbbbbbbb-bbbb-4ccc-8ddd-bbbbbbbbbbbb"
	artID := "cccccccc-cccc-4ccc-8ddd-cccccccccccc"
	verID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	verID2 := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"

	a := domain.Asset{
		ID: sourceID, Type: domain.AssetTypeSource,
		StorageKey: "private/blobs/metaldogs/source/2026/11/" + sourceID + ".jpg",
		MimeType:   "image/jpeg", SizeBytes: 1, Width: 300, Height: 300,
		Checksum: application.ChecksumSHA256Prefix + "ab", CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
	}
	if err := assets.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("token-hash-bytes-for-approve!!!!!!"))
	d := domain.Design{
		ID: designID, TemplateID: "ffffffff-ffff-4fff-8fff-ffffffffffff", TemplateVersion: 1,
		Status: domain.DesignDraft, Texts: map[string]string{"dog_name": "Burek"},
		SourceAssetID: sourceID, OwnerTokenHash: sum[:],
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(domain.DesignRetentionUnordered),
	}
	if err := designs.CreateBound(ctx, d, now.Add(domain.SourceAttachedRetention)); err != nil {
		t.Fatal(err)
	}
	art := domain.Asset{
		ID: artID, Type: domain.AssetTypeArtwork, OwnerRef: designID,
		StorageKey: "private/blobs/metaldogs/artwork/2026/11/" + artID + ".png",
		MimeType:   "image/png", SizeBytes: 1, Width: 64, Height: 64,
		Checksum: application.ChecksumSHA256Prefix + "art", CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
	}
	if err := assets.Insert(ctx, art); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE metaldogs_designs SET status = $1, updated_at = $2 WHERE id = $3
	`, domain.DesignPreviewReady, now, designID); err != nil {
		t.Fatal(err)
	}
	d.Status = domain.DesignPreviewReady
	d.UpdatedAt = now
	if err := d.Approve(verID, now); err != nil {
		t.Fatal(err)
	}
	v, err := domain.NewDesignVersion(
		verID, designID, d.TemplateID, d.TemplateVersion, d.Texts,
		artID, art.Checksum, "fake", "fake", "pv1", []byte(`{}`), now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := versions.ApproveBound(ctx, d, v); err != nil {
		t.Fatal(err)
	}
	gotDes, ok, err := designs.GetByID(ctx, designID)
	if err != nil || !ok || gotDes.Status != domain.DesignApproved || gotDes.ApprovedVersionID != verID {
		t.Fatalf("%+v ok=%v err=%v", gotDes, ok, err)
	}
	if !gotDes.ExpiresAt.Equal(d.ExpiresAt) {
		t.Fatalf("expires slid: %v want %v", gotDes.ExpiresAt, d.ExpiresAt)
	}
	gotArt, ok, err := assets.GetByID(ctx, artID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if gotArt.DeleteAfter.Before(gotDes.ExpiresAt) {
		t.Fatalf("artwork delete_after %v must reach expires_at %v", gotArt.DeleteAfter, gotDes.ExpiresAt)
	}
	gotVer, ok, err := versions.GetByDesignID(ctx, designID)
	if err != nil || !ok || gotVer.ID != verID || gotVer.ArtworkChecksum != art.Checksum {
		t.Fatalf("%+v ok=%v err=%v", gotVer, ok, err)
	}

	// Unique(design_id) conflict after approved.
	v2, err := domain.NewDesignVersion(
		verID2, designID, d.TemplateID, d.TemplateVersion, d.Texts,
		artID, art.Checksum, "fake", "fake", "pv1", nil, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	stale := d
	stale.ApprovedVersionID = verID2
	if err := versions.ApproveBound(ctx, stale, v2); err != ports.ErrDesignConflict {
		t.Fatalf("want conflict got %v", err)
	}

	// Status race with free unique index: another design not preview_ready.
	source2 := "aaaaaaaa-bbbb-4ccc-8ddd-bbbbbbbbbbbb"
	design2 := "bbbbbbbb-bbbb-4ccc-8ddd-cccccccccccc"
	art2 := "cccccccc-cccc-4ccc-8ddd-dddddddddddd"
	ver3 := "dddddddd-dddd-4ddd-8ddd-eeeeeeeeeeee"
	a2 := a
	a2.ID = source2
	a2.StorageKey = "private/blobs/metaldogs/source/2026/11/" + source2 + ".jpg"
	if err := assets.Insert(ctx, a2); err != nil {
		t.Fatal(err)
	}
	d2 := domain.Design{
		ID: design2, TemplateID: d.TemplateID, TemplateVersion: 1,
		Status: domain.DesignDraft, Texts: map[string]string{"dog_name": "Azor"},
		SourceAssetID: source2, OwnerTokenHash: sum[:],
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(domain.DesignRetentionUnordered),
	}
	if err := designs.CreateBound(ctx, d2, now.Add(domain.SourceAttachedRetention)); err != nil {
		t.Fatal(err)
	}
	artRow2 := art
	artRow2.ID = art2
	artRow2.OwnerRef = design2
	artRow2.StorageKey = "private/blobs/metaldogs/artwork/2026/11/" + art2 + ".png"
	if err := assets.Insert(ctx, artRow2); err != nil {
		t.Fatal(err)
	}
	// Stay draft (not preview_ready) — ApproveBound must conflict before insert.
	d2.ApprovedVersionID = ver3
	d2.UpdatedAt = now
	d2.ExpiresAt = now.Add(domain.DesignRetentionUnordered)
	v3, err := domain.NewDesignVersion(
		ver3, design2, d2.TemplateID, 1, d2.Texts, art2, artRow2.Checksum, "fake", "fake", "pv1", nil, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := versions.ApproveBound(ctx, d2, v3); err != ports.ErrDesignConflict {
		t.Fatalf("status race want conflict got %v", err)
	}
	if _, ok, err := versions.GetByDesignID(ctx, design2); err != nil || ok {
		t.Fatal("no version row on status race")
	}

	// Append-only: frozen column update rejected.
	if _, err := db.ExecContext(ctx, `
		UPDATE metaldogs_design_versions SET artwork_checksum = 'sha256:tampered' WHERE id = $1
	`, verID); err == nil {
		t.Fatal("append-only must reject checksum update")
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE metaldogs_design_versions SET production_status = 'preparing' WHERE id = $1
	`, verID); err != nil {
		t.Fatalf("production_status update allowed: %v", err)
	}

	// ID mismatch rejected.
	bad := d
	bad.ID = designID
	bad.ApprovedVersionID = verID
	vBad := v
	vBad.DesignID = design2
	if err := versions.ApproveBound(ctx, bad, vBad); err == nil {
		t.Fatal("mismatched design ids must fail")
	}
}
