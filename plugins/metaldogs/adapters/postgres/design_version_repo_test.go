package postgres_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
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

func TestDesignVersionRepo_ProductionTransitionAndAppendOnly(t *testing.T) {
	db := testutil.OpenIsolated(t, "design_version_production")
	ctx := context.Background()
	versions := postgres.NewDesignVersionRepo(db)
	assets := postgres.NewAssetRepo(db)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	failed := seedApprovedVersion(t, db, "a")
	if _, err := db.ExecContext(ctx, `
		UPDATE metaldogs_design_versions SET artwork_checksum = 'sha256:tampered' WHERE id = $1
	`, failed.versionID); err == nil {
		t.Fatal("artwork_checksum must stay immutable")
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE metaldogs_design_versions SET production_failure_reason = 'manual_reason' WHERE id = $1
	`, failed.versionID); err != nil {
		t.Fatalf("production_failure_reason update: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE metaldogs_design_versions
		SET provider = 'other', production_failure_reason = 'too_small'
		WHERE id = $1
	`, failed.versionID); err == nil {
		t.Fatal("frozen column must stay immutable when written with production_failure_reason")
	}
	var reason, provider string
	if err := db.QueryRowContext(ctx, `
		SELECT production_failure_reason, provider FROM metaldogs_design_versions WHERE id = $1
	`, failed.versionID).Scan(&reason, &provider); err != nil {
		t.Fatal(err)
	}
	if reason != "manual_reason" || provider != "fake" {
		t.Fatalf("reason=%s provider=%s", reason, provider)
	}
	if err := versions.MarkProductionPreparing(ctx, failed.versionID); err != nil {
		t.Fatal(err)
	}
	if err := versions.MarkProductionFailed(ctx, failed.versionID, "too_small"); err != nil {
		t.Fatal(err)
	}
	if err := versions.MarkProductionFailed(ctx, failed.versionID, "aspect_mismatch"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := versions.GetByID(ctx, failed.versionID)
	if err != nil || !ok || got.ProductionStatus != domain.ProductionFailed || got.ProductionFailureReason != "too_small" {
		t.Fatalf("%+v ok=%v err=%v", got, ok, err)
	}
	if got.ArtworkChecksum != failed.artworkChecksum {
		t.Fatalf("artwork checksum changed: %s", got.ArtworkChecksum)
	}
	if err := versions.MarkProductionPreparing(ctx, failed.versionID); err != ports.ErrProductionNotOpen {
		t.Fatalf("preparing from failed: %v", err)
	}
	readyAsset := productionAsset(failed.designID, "11111111-1111-4111-8111-111111111111", now)
	if err := versions.CommitProductionReady(ctx, failed.versionID, readyAsset); err != ports.ErrProductionNotOpen {
		t.Fatalf("ready from failed: %v", err)
	}
	if n := countProductionAssets(t, db, failed.designID); n != 0 {
		t.Fatalf("assets=%d", n)
	}

	ready := seedApprovedVersion(t, db, "b")
	if err := versions.MarkProductionPreparing(ctx, ready.versionID); err != nil {
		t.Fatal(err)
	}
	if err := versions.MarkProductionPreparing(ctx, ready.versionID); err != nil {
		t.Fatal(err)
	}
	asset := productionAsset(ready.designID, "22222222-2222-4222-8222-222222222222", now)
	if err := versions.CommitProductionReady(ctx, ready.versionID, asset); err != nil {
		t.Fatal(err)
	}
	got, ok, err = versions.GetByID(ctx, ready.versionID)
	if err != nil || !ok || got.ProductionStatus != domain.ProductionReady || got.ProductionAssetID != asset.ID ||
		got.ProductionChecksum != asset.Checksum || got.ProductionFailureReason != "" || got.ArtworkChecksum != ready.artworkChecksum {
		t.Fatalf("%+v ok=%v err=%v", got, ok, err)
	}
	stored, ok, err := assets.GetByID(ctx, asset.ID)
	if err != nil || !ok || stored.Checksum != asset.Checksum || stored.Type != domain.AssetTypeProduction || stored.OwnerRef != ready.designID {
		t.Fatalf("%+v ok=%v err=%v", stored, ok, err)
	}
	if err := versions.CommitProductionReady(ctx, ready.versionID, asset); err != ports.ErrProductionAlreadyReady {
		t.Fatalf("second ready: %v", err)
	}
	if n := countProductionAssets(t, db, ready.designID); n != 1 {
		t.Fatalf("assets=%d", n)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE metaldogs_design_versions SET texts = '{"dog_name":"x"}'::jsonb WHERE id = $1
	`, ready.versionID); err == nil {
		t.Fatal("texts must stay immutable after ready")
	}

	pending := seedApprovedVersion(t, db, "c")
	direct := productionAsset(pending.designID, "33333333-3333-4333-8333-333333333333", now)
	if err := versions.CommitProductionReady(ctx, pending.versionID, direct); err != nil {
		t.Fatal(err)
	}
	got, ok, err = versions.GetByID(ctx, pending.versionID)
	if err != nil || !ok || got.ProductionStatus != domain.ProductionReady || got.ProductionAssetID != direct.ID {
		t.Fatalf("%+v ok=%v err=%v", got, ok, err)
	}
}

type seededVersion struct {
	designID        string
	versionID       string
	artworkChecksum string
}

func seedApprovedVersion(t *testing.T, db *sql.DB, tag string) seededVersion {
	t.Helper()
	ctx := context.Background()
	assets := postgres.NewAssetRepo(db)
	designs := postgres.NewDesignRepo(db)
	versions := postgres.NewDesignVersionRepo(db)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	// tag is one hex digit; the last group stays 12 digits.
	sourceID := "a0000000-0000-4000-8000-00000000000" + tag
	designID := "b0000000-0000-4000-8000-00000000000" + tag
	artID := "c0000000-0000-4000-8000-00000000000" + tag
	verID := "d0000000-0000-4000-8000-00000000000" + tag
	if err := assets.Insert(ctx, domain.Asset{
		ID: sourceID, Type: domain.AssetTypeSource,
		StorageKey: "private/blobs/metaldogs/source/2026/11/" + sourceID + ".jpg",
		MimeType:   "image/jpeg", SizeBytes: 1, Width: 300, Height: 300,
		Checksum: application.ChecksumSHA256Prefix + "ab", CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("token-" + tag))
	d := domain.Design{
		ID: designID, TemplateID: "ffffffff-ffff-4fff-8fff-ffffffffffff", TemplateVersion: 1,
		Status: domain.DesignDraft, Texts: map[string]string{"dog_name": "Burek"},
		SourceAssetID: sourceID, OwnerTokenHash: sum[:],
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(domain.DesignRetentionUnordered),
	}
	if err := designs.CreateBound(ctx, d, now.Add(domain.SourceAttachedRetention)); err != nil {
		t.Fatal(err)
	}
	checksum := application.ChecksumSHA256Prefix + "art" + tag
	if err := assets.Insert(ctx, domain.Asset{
		ID: artID, Type: domain.AssetTypeArtwork, OwnerRef: designID,
		StorageKey: "private/blobs/metaldogs/artwork/2026/11/" + artID + ".png",
		MimeType:   "image/png", SizeBytes: 1, Width: 64, Height: 64,
		Checksum: checksum, CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE metaldogs_designs SET status = $1, updated_at = $2 WHERE id = $3
	`, domain.DesignPreviewReady, now, designID); err != nil {
		t.Fatal(err)
	}
	d.Status = domain.DesignPreviewReady
	if err := d.Approve(verID, now); err != nil {
		t.Fatal(err)
	}
	v, err := domain.NewDesignVersion(
		verID, designID, d.TemplateID, d.TemplateVersion, d.Texts,
		artID, checksum, "fake", "fake", "pv1", []byte(`{}`), now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := versions.ApproveBound(ctx, d, v); err != nil {
		t.Fatal(err)
	}
	return seededVersion{designID: designID, versionID: verID, artworkChecksum: checksum}
}

func productionAsset(designID, id string, now time.Time) domain.Asset {
	return domain.Asset{
		ID: id, Type: domain.AssetTypeProduction, OwnerRef: designID,
		StorageKey: "private/blobs/metaldogs/production/2026/11/" + id + ".png",
		MimeType:   "image/png", SizeBytes: 4, Width: 8, Height: 8,
		Checksum: application.ChecksumSHA256Prefix + id, CreatedAt: now, DeleteAfter: now.Add(time.Hour),
	}
}

func countProductionAssets(t *testing.T, db *sql.DB, designID string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM metaldogs_assets WHERE owner_ref = $1 AND type = 'production' AND deleted_at IS NULL
	`, designID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
