package postgres_test

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestRetention_ReferencedAssetsSurviveEachPath(t *testing.T) {
	db := testutil.OpenIsolated(t, "retention_keep")
	ctx := context.Background()
	now := time.Date(2026, 11, 23, 4, 0, 0, 0, time.UTC)

	t.Run("asset cleanup", func(t *testing.T) {
		art, prod, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
		insertRetentionAsset(t, db, art, domain.AssetTypeArtwork, "design-a", now, now.Add(-time.Minute))
		insertRetentionAsset(t, db, prod, domain.AssetTypeProduction, "design-a", now, now)
		insertRetentionAsset(t, db, other, domain.AssetTypeMockup, "design-a", now, now.Add(-time.Minute))
		future := uuid.NewString()
		insertRetentionAsset(t, db, future, domain.AssetTypeSource, "", now, now.Add(time.Minute))
		insertRetentionSnapshot(t, db, art, prod, now)
		buf := &bytes.Buffer{}
		log := logger.NewWithWriter(buf, "info")
		if err := postgres.CleanupDueAssets(ctx, db, now, log); err != nil {
			t.Fatal(err)
		}
		assertAssetActive(t, db, art, true)
		assertAssetActive(t, db, prod, true)
		assertAssetActive(t, db, other, false)
		assertAssetActive(t, db, future, true)
		assertKeptOnce(t, buf.String(), postgres.RetentionPathAssetCleanup, art, prod)
		if strings.Contains(buf.String(), other) || strings.Contains(buf.String(), future) {
			t.Fatalf("log mentioned an unprotected asset: %s", buf.String())
		}
	})

	t.Run("unattached upload cleanup", func(t *testing.T) {
		art, prod, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
		attached := uuid.NewString()
		insertRetentionAsset(t, db, art, domain.AssetTypeArtwork, "", now, now.Add(-time.Minute))
		insertRetentionAsset(t, db, prod, domain.AssetTypeProduction, "", now, now.Add(-time.Minute))
		insertRetentionAsset(t, db, other, domain.AssetTypeSource, "", now, now.Add(-time.Minute))
		insertRetentionAsset(t, db, attached, domain.AssetTypeSource, "design-b", now, now.Add(-time.Minute))
		insertRetentionSnapshot(t, db, art, prod, now)
		buf := &bytes.Buffer{}
		log := logger.NewWithWriter(buf, "info")
		if err := postgres.CleanupUnattachedUploads(ctx, db, now, log); err != nil {
			t.Fatal(err)
		}
		assertAssetActive(t, db, art, true)
		assertAssetActive(t, db, prod, true)
		assertAssetActive(t, db, other, false)
		assertAssetActive(t, db, attached, true)
		assertKeptOnce(t, buf.String(), postgres.RetentionPathUnattachedUploads, art, prod)
	})

	t.Run("design expiry", func(t *testing.T) {
		designID, sourceID, art, prod := seedExpiringDesign(t, db, now)
		orderedID, orderedSource := seedOrderedDesign(t, db, now)
		insertRetentionSnapshot(t, db, art, prod, now)
		buf := &bytes.Buffer{}
		log := logger.NewWithWriter(buf, "info")
		if err := postgres.ExpireDesigns(ctx, db, now, log); err != nil {
			t.Fatal(err)
		}
		assertAssetActive(t, db, art, true)
		assertAssetActive(t, db, prod, true)
		assertAssetActive(t, db, sourceID, false)
		assertDesignStatus(t, db, designID, domain.DesignExpired)
		assertAssetActive(t, db, orderedSource, true)
		assertDesignStatus(t, db, orderedID, domain.DesignOrdered)
		assertKeptOnce(t, buf.String(), postgres.RetentionPathDesignExpiry, art, prod)
		if strings.Contains(buf.String(), sourceID) || strings.Contains(buf.String(), orderedSource) {
			t.Fatalf("log mentioned an unprotected asset: %s", buf.String())
		}
	})
}

func TestRetention_MockupAndThumbnailSurviveEachPath(t *testing.T) {
	db := testutil.OpenIsolated(t, "retention_preview")
	ctx := context.Background()
	now := time.Date(2026, 11, 23, 5, 0, 0, 0, time.UTC)

	t.Run("asset cleanup", func(t *testing.T) {
		mockID, thumbID, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
		insertRetentionAsset(t, db, mockID, domain.AssetTypeMockup, "design-m", now, now.Add(-time.Minute))
		insertRetentionAsset(t, db, thumbID, domain.AssetTypeThumbnail, "design-m", now, now.Add(-time.Minute))
		insertRetentionAsset(t, db, other, domain.AssetTypeReference, "design-m", now, now.Add(-time.Minute))
		insertRetentionPreview(t, db, mockID, thumbID, now)
		buf := &bytes.Buffer{}
		if err := postgres.CleanupDueAssets(ctx, db, now, logger.NewWithWriter(buf, "info")); err != nil {
			t.Fatal(err)
		}
		assertAssetActive(t, db, mockID, true)
		assertAssetActive(t, db, thumbID, true)
		assertAssetActive(t, db, other, false)
		assertKeptOnce(t, buf.String(), postgres.RetentionPathAssetCleanup, mockID, thumbID)
	})

	t.Run("unattached upload cleanup", func(t *testing.T) {
		mockID, thumbID, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
		insertRetentionAsset(t, db, mockID, domain.AssetTypeMockup, "", now, now.Add(-time.Minute))
		insertRetentionAsset(t, db, thumbID, domain.AssetTypeThumbnail, "", now, now.Add(-time.Minute))
		insertRetentionAsset(t, db, other, domain.AssetTypeSource, "", now, now.Add(-time.Minute))
		insertRetentionPreview(t, db, mockID, thumbID, now)
		buf := &bytes.Buffer{}
		if err := postgres.CleanupUnattachedUploads(ctx, db, now, logger.NewWithWriter(buf, "info")); err != nil {
			t.Fatal(err)
		}
		assertAssetActive(t, db, mockID, true)
		assertAssetActive(t, db, thumbID, true)
		assertAssetActive(t, db, other, false)
		assertKeptOnce(t, buf.String(), postgres.RetentionPathUnattachedUploads, mockID, thumbID)
	})

	t.Run("design expiry", func(t *testing.T) {
		designID, sourceID, _, _ := seedExpiringDesign(t, db, now)
		mockID, thumbID, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
		insertRetentionAsset(t, db, mockID, domain.AssetTypeMockup, designID, now, now.Add(-time.Minute))
		insertRetentionAsset(t, db, thumbID, domain.AssetTypeThumbnail, designID, now, now.Add(-time.Minute))
		insertRetentionAsset(t, db, other, domain.AssetTypeReference, designID, now, now.Add(-time.Minute))
		insertRetentionPreview(t, db, mockID, thumbID, now)
		buf := &bytes.Buffer{}
		if err := postgres.ExpireDesigns(ctx, db, now, logger.NewWithWriter(buf, "info")); err != nil {
			t.Fatal(err)
		}
		assertAssetActive(t, db, mockID, true)
		assertAssetActive(t, db, thumbID, true)
		assertAssetActive(t, db, other, false)
		assertAssetActive(t, db, sourceID, false)
		assertDesignStatus(t, db, designID, domain.DesignExpired)
		assertKeptOnce(t, buf.String(), postgres.RetentionPathDesignExpiry, mockID, thumbID)
	})

	t.Run("soft delete", func(t *testing.T) {
		mockID, thumbID, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
		insertRetentionAsset(t, db, mockID, domain.AssetTypeMockup, "", now, now.Add(time.Hour))
		insertRetentionAsset(t, db, thumbID, domain.AssetTypeThumbnail, "", now, now.Add(time.Hour))
		insertRetentionAsset(t, db, other, domain.AssetTypeArtwork, "", now, now.Add(time.Hour))
		insertRetentionPreview(t, db, mockID, thumbID, now)
		buf := &bytes.Buffer{}
		repo := postgres.NewAssetRepo(db)
		repo.SetKeptLog(logger.NewWithWriter(buf, "info"))
		if err := repo.SoftDelete(ctx, mockID, now); err != nil {
			t.Fatal(err)
		}
		if err := repo.SoftDelete(ctx, thumbID, now); err != nil {
			t.Fatal(err)
		}
		if err := repo.SoftDelete(ctx, other, now); err != nil {
			t.Fatal(err)
		}
		assertAssetActive(t, db, mockID, true)
		assertAssetActive(t, db, thumbID, true)
		assertAssetActive(t, db, other, false)
		assertKeptOnce(t, buf.String(), "soft_delete", mockID, thumbID)
	})

	t.Run("replace source", func(t *testing.T) {
		for _, column := range []string{"mockup", "thumbnail"} {
			t.Run(column, func(t *testing.T) {
				designID, sourceID := seedDraftDesign(t, db, now)
				other := uuid.NewString()
				insertRetentionAsset(t, db, other, domain.AssetTypeArtwork, "", now, now.Add(time.Hour))
				mockID, thumbID := other, sourceID
				if column == "mockup" {
					mockID, thumbID = sourceID, other
				}
				insertRetentionPreview(t, db, mockID, thumbID, now)
				fresh := uuid.NewString()
				insertRetentionAsset(t, db, fresh, domain.AssetTypeSource, "", now, now.Add(time.Hour))
				got, ok, err := postgres.NewDesignRepo(db).GetByID(ctx, designID)
				if err != nil || !ok {
					t.Fatalf("design ok=%v err=%v", ok, err)
				}
				buf := &bytes.Buffer{}
				designs := postgres.NewDesignRepo(db)
				designs.SetKeptLog(logger.NewWithWriter(buf, "info"))
				expected := got.UpdatedAt
				got.SourceAssetID = fresh
				got.UpdatedAt = now.Add(time.Minute)
				if _, err := designs.ReplaceSource(ctx, got, sourceID, expected, now.Add(time.Hour)); err == nil {
					t.Fatal("replace of a snapshot-referenced source must not commit")
				}
				assertAssetActive(t, db, sourceID, true)
				assertDesignStatus(t, db, designID, domain.DesignDraft)
				assertKeptOnce(t, buf.String(), "replace_source", sourceID)
			})
		}
	})
}

func TestRetention_NoSnapshotStillDeletes(t *testing.T) {
	db := testutil.OpenIsolated(t, "retention_open")
	ctx := context.Background()
	now := time.Date(2026, 11, 23, 4, 5, 0, 0, time.UTC)
	log := logger.NewWithWriter(&bytes.Buffer{}, "info")

	art := uuid.NewString()
	insertRetentionAsset(t, db, art, domain.AssetTypeArtwork, "design-c", now, now.Add(-time.Minute))
	if err := postgres.CleanupDueAssets(ctx, db, now, log); err != nil {
		t.Fatal(err)
	}
	assertAssetActive(t, db, art, false)

	loose := uuid.NewString()
	insertRetentionAsset(t, db, loose, domain.AssetTypeSource, "", now, now.Add(-time.Minute))
	if err := postgres.CleanupUnattachedUploads(ctx, db, now, log); err != nil {
		t.Fatal(err)
	}
	assertAssetActive(t, db, loose, false)

	designID, sourceID, artID, prodID := seedExpiringDesign(t, db, now)
	buf := &bytes.Buffer{}
	if err := postgres.ExpireDesigns(ctx, db, now, logger.NewWithWriter(buf, "info")); err != nil {
		t.Fatal(err)
	}
	assertDesignStatus(t, db, designID, domain.DesignExpired)
	assertAssetActive(t, db, sourceID, false)
	assertAssetActive(t, db, artID, false)
	assertAssetActive(t, db, prodID, false)
	if buf.Len() != 0 {
		t.Fatalf("expiry without snapshots logged: %s", buf.String())
	}
}

func TestRetention_DeleteWaitsForShareLockThenKeepsReference(t *testing.T) {
	db := testutil.OpenIsolated(t, "retention_lock")
	ctx := context.Background()
	now := time.Date(2026, 11, 23, 4, 20, 0, 0, time.UTC)

	t.Run("soft delete", func(t *testing.T) {
		id := uuid.NewString()
		insertRetentionAsset(t, db, id, domain.AssetTypeArtwork, "", now, now.Add(time.Hour))
		release := holdAssetShare(t, db, id)
		done := start(t, func() error {
			return postgres.NewAssetRepo(db).SoftDelete(ctx, id, now)
		})
		waitForAssetLockWait(t, db, release.pid)
		insertRetentionSnapshot(t, db, id, "", now)
		release.commit()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		assertAssetActive(t, db, id, true)
	})

	t.Run("replace source", func(t *testing.T) {
		designID, sourceID := seedDraftDesign(t, db, now)
		fresh := uuid.NewString()
		insertRetentionAsset(t, db, fresh, domain.AssetTypeSource, "", now, now.Add(time.Hour))
		got, ok, err := postgres.NewDesignRepo(db).GetByID(ctx, designID)
		if err != nil || !ok {
			t.Fatalf("design ok=%v err=%v", ok, err)
		}
		expectedUpdated := got.UpdatedAt
		release := holdAssetShare(t, db, sourceID)
		done := start(t, func() error {
			got.SourceAssetID = fresh
			got.UpdatedAt = now.Add(time.Minute)
			_, err := postgres.NewDesignRepo(db).ReplaceSource(ctx, got, sourceID, expectedUpdated, now.Add(time.Hour))
			return err
		})
		waitForAssetLockWait(t, db, release.pid)
		insertRetentionSnapshot(t, db, sourceID, "", now)
		release.commit()
		if err := <-done; err == nil {
			t.Fatal("replace of a snapshot-referenced source must not commit")
		}
		assertAssetActive(t, db, sourceID, true)
		assertDesignStatus(t, db, designID, domain.DesignDraft)
	})
}

type shareHold struct {
	pid    int
	commit func()
}

func holdAssetShare(t *testing.T, db *sql.DB, id string) shareHold {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	if err := tx.QueryRow(`SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := tx.QueryRow(`
		SELECT id::text FROM metaldogs_assets WHERE id = $1::uuid FOR SHARE
	`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return shareHold{pid: pid, commit: func() {
		t.Helper()
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}}
}

func start(t *testing.T, fn func() error) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	return done
}

func waitForAssetLockWait(t *testing.T, db *sql.DB, holderPID int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		err := db.QueryRow(`
			SELECT COUNT(*) FROM pg_stat_activity
			WHERE pid <> $1
			  AND wait_event_type = 'Lock'
			  AND query LIKE '%metaldogs_assets%'
			  AND query LIKE '%FOR UPDATE%'
		`, holderPID).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("deletion did not wait on the asset row lock")
}

func TestRetention_SoftDeleteAndReplaceSkipReferenced(t *testing.T) {
	db := testutil.OpenIsolated(t, "retention_soft")
	ctx := context.Background()
	now := time.Date(2026, 11, 23, 4, 10, 0, 0, time.UTC)

	art := uuid.NewString()
	other := uuid.NewString()
	insertRetentionAsset(t, db, art, domain.AssetTypeArtwork, "", now, now.Add(time.Hour))
	insertRetentionAsset(t, db, other, domain.AssetTypeArtwork, "", now, now.Add(time.Hour))
	insertRetentionSnapshot(t, db, art, "", now)
	buf := &bytes.Buffer{}
	repo := postgres.NewAssetRepo(db)
	repo.SetKeptLog(logger.NewWithWriter(buf, "info"))
	if err := repo.SoftDelete(ctx, art, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.SoftDelete(ctx, other, now); err != nil {
		t.Fatal(err)
	}
	assertAssetActive(t, db, art, true)
	assertAssetActive(t, db, other, false)
	assertKeptOnce(t, buf.String(), "soft_delete", art)

	designID, sourceID := seedDraftDesign(t, db, now)
	insertRetentionSnapshot(t, db, sourceID, "", now)
	fresh := uuid.NewString()
	insertRetentionAsset(t, db, fresh, domain.AssetTypeSource, "", now, now.Add(time.Hour))
	got, ok, err := postgres.NewDesignRepo(db).GetByID(ctx, designID)
	if err != nil || !ok {
		t.Fatalf("design ok=%v err=%v", ok, err)
	}
	replaceBuf := &bytes.Buffer{}
	designs := postgres.NewDesignRepo(db)
	designs.SetKeptLog(logger.NewWithWriter(replaceBuf, "info"))
	expectedUpdated := got.UpdatedAt
	got.SourceAssetID = fresh
	got.UpdatedAt = now.Add(time.Minute)
	if _, err := designs.ReplaceSource(ctx, got, sourceID, expectedUpdated, now.Add(time.Hour)); err == nil {
		t.Fatal("replace of a snapshot-referenced source must not commit")
	}
	assertAssetActive(t, db, sourceID, true)
	assertDesignStatus(t, db, designID, domain.DesignDraft)
	reloaded, ok, err := designs.GetByID(ctx, designID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if reloaded.SourceAssetID != sourceID {
		t.Fatalf("source changed to %s", reloaded.SourceAssetID)
	}
	assertKeptOnce(t, replaceBuf.String(), "replace_source", sourceID)
}

func insertRetentionAsset(t *testing.T, db *sql.DB, id, assetType, owner string, created, deleteAfter time.Time) {
	t.Helper()
	if err := postgres.NewAssetRepo(db).Insert(context.Background(), domain.Asset{
		ID: id, Type: assetType, OwnerRef: owner,
		StorageKey: "private/blobs/metaldogs/" + assetType + "/" + id + ".png",
		MimeType:   "image/png", SizeBytes: 1, Width: 8, Height: 8,
		Checksum: "sha256:" + id, CreatedAt: created, DeleteAfter: deleteAfter,
	}); err != nil {
		t.Fatal(err)
	}
}

func insertRetentionPreview(t *testing.T, db *sql.DB, mockID, thumbID string, at time.Time) {
	t.Helper()
	snap, err := domain.NewOrderDesignSnapshot(
		uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), "TEE-M",
		"black-tee", 1, map[string]string{"dog_name": "Burek"},
		"openai", "gpt-image", "prompt-v1",
		uuid.NewString(), "sha256:art", "", "", at,
	)
	if err != nil {
		t.Fatal(err)
	}
	snap, err = snap.WithThumbnail(mockID, thumbID)
	if err != nil {
		t.Fatal(err)
	}
	written, err := postgres.NewOrderDesignSnapshotRepo(db).Insert(context.Background(), snap)
	if err != nil || !written {
		t.Fatalf("snapshot written=%v err=%v", written, err)
	}
}

func insertRetentionSnapshot(t *testing.T, db *sql.DB, artID, prodID string, at time.Time) {
	t.Helper()
	prodSum := ""
	if prodID != "" {
		prodSum = "sha256:prod"
	}
	snap, err := domain.NewOrderDesignSnapshot(
		uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), "TEE-M",
		"black-tee", 1, map[string]string{"dog_name": "Burek"},
		"openai", "gpt-image", "prompt-v1",
		artID, "sha256:art", prodID, prodSum, at,
	)
	if err != nil {
		t.Fatal(err)
	}
	written, err := postgres.NewOrderDesignSnapshotRepo(db).Insert(context.Background(), snap)
	if err != nil || !written {
		t.Fatalf("snapshot written=%v err=%v", written, err)
	}
}

func seedExpiringDesign(t *testing.T, db *sql.DB, now time.Time) (designID, sourceID, artID, prodID string) {
	t.Helper()
	designID, sourceID = insertDesignWithSource(t, db, domain.DesignApproved, now.Add(-time.Hour), now)
	artID, prodID = uuid.NewString(), uuid.NewString()
	insertRetentionAsset(t, db, artID, domain.AssetTypeArtwork, designID, now, now.Add(-time.Minute))
	insertRetentionAsset(t, db, prodID, domain.AssetTypeProduction, designID, now, now.Add(-time.Minute))
	return designID, sourceID, artID, prodID
}

func seedOrderedDesign(t *testing.T, db *sql.DB, now time.Time) (designID, sourceID string) {
	t.Helper()
	return insertDesignWithSource(t, db, domain.DesignOrdered, now.Add(-time.Hour), now)
}

func seedDraftDesign(t *testing.T, db *sql.DB, now time.Time) (designID, sourceID string) {
	t.Helper()
	return insertDesignWithSource(t, db, domain.DesignDraft, now.Add(24*time.Hour), now)
}

func insertDesignWithSource(t *testing.T, db *sql.DB, status string, expires, now time.Time) (designID, sourceID string) {
	t.Helper()
	designID, sourceID = uuid.NewString(), uuid.NewString()
	insertRetentionAsset(t, db, sourceID, domain.AssetTypeSource, "", now, now.Add(-time.Minute))
	err := postgres.NewDesignRepo(db).CreateBound(context.Background(), domain.Design{
		ID: designID, TemplateID: uuid.NewString(), TemplateVersion: 1, Status: status,
		Texts: map[string]string{"dog_name": "Burek"}, SourceAssetID: sourceID,
		OwnerTokenHash: bytes.Repeat([]byte{7}, 32),
		CreatedAt:      now, UpdatedAt: now, ExpiresAt: expires,
	}, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return designID, sourceID
}

func assertAssetActive(t *testing.T, db *sql.DB, id string, want bool) {
	t.Helper()
	var deleted sql.NullTime
	if err := db.QueryRow(`SELECT deleted_at FROM metaldogs_assets WHERE id = $1`, id).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	if deleted.Valid == want {
		t.Fatalf("asset %s active=%v deleted_at=%v", id, !deleted.Valid, deleted.Time)
	}
}

func assertDesignStatus(t *testing.T, db *sql.DB, id, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT status FROM metaldogs_designs WHERE id = $1`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("design %s status %s want %s", id, got, want)
	}
}

func assertKeptOnce(t *testing.T, buf, path string, assetIDs ...string) {
	t.Helper()
	lines := 0
	for _, line := range strings.Split(buf, "\n") {
		if strings.Contains(line, postgres.RetentionAssetKeptEvent) {
			lines++
		}
	}
	if lines != len(assetIDs) {
		t.Fatalf("kept lines=%d want %d buf=%s", lines, len(assetIDs), buf)
	}
	for _, id := range assetIDs {
		n := 0
		for _, line := range strings.Split(buf, "\n") {
			if strings.Contains(line, postgres.RetentionAssetKeptEvent) &&
				strings.Contains(line, id) && strings.Contains(line, path) {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("asset %s path %s logged %d times: %s", id, path, n, buf)
		}
	}
}
