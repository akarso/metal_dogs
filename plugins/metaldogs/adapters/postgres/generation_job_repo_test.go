package postgres_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func seedDesignForJobs(t *testing.T, designs *postgres.DesignRepo, assets *postgres.AssetRepo, designID, assetID string, now time.Time) {
	t.Helper()
	ctx := context.Background()
	a := domain.Asset{
		ID: assetID, Type: domain.AssetTypeSource,
		StorageKey: "private/blobs/metaldogs/source/2026/11/" + assetID + ".jpg",
		MimeType:   "image/jpeg", SizeBytes: 1, Width: 300, Height: 300,
		Checksum: application.ChecksumSHA256Prefix + "ab", CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
	}
	if err := assets.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("token-hash-bytes-for-generation!!!!"))
	d := domain.Design{
		ID: designID, TemplateID: "cccccccc-cccc-4ccc-8ddd-cccccccccccc", TemplateVersion: 1,
		Status: domain.DesignDraft, Texts: map[string]string{"dog_name": "Burek"},
		SourceAssetID: assetID, OwnerTokenHash: sum[:],
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(domain.DesignRetentionUnordered),
	}
	if err := designs.CreateBound(ctx, d, now.Add(domain.SourceAttachedRetention)); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationJobRepo_BeginIdempotencyAndMutex(t *testing.T) {
	db := testutil.OpenIsolated(t, "generation_jobs")
	assets := postgres.NewAssetRepo(db)
	designs := postgres.NewDesignRepo(db)
	jobs := postgres.NewGenerationJobRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	designID := "bbbbbbbb-bbbb-4ccc-8ddd-bbbbbbbbbbbb"
	assetID := "aaaaaaaa-bbbb-4ccc-8ddd-aaaaaaaaaaaa"
	seedDesignForJobs(t, designs, assets, designID, assetID, now)

	d, ok, err := designs.GetByID(ctx, designID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	jobID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	if err := d.RequestGeneration(jobID, 3, now); err != nil {
		t.Fatal(err)
	}
	digest := domain.TextsDigestFor(d.Texts)
	job := domain.NewQueuedJob(jobID, designID, "idem-a", "fake", d.GenerationAttempts, now, "corr", d.SourceAssetID, d.TemplateVersion, digest)
	if err := jobs.BeginGenerating(ctx, job, d, now.Add(domain.SourceAttachedRetention), d.GenerationAttempts-1, ""); err != nil {
		t.Fatal(err)
	}

	got, ok, err := jobs.GetByID(ctx, jobID)
	if err != nil || !ok || got.Status != domain.JobQueued || got.IdempotencyKey != "idem-a" || got.TextsDigest != digest {
		t.Fatalf("%+v ok=%v err=%v", got, ok, err)
	}
	byKey, ok, err := jobs.GetByIdempotency(ctx, designID, "idem-a")
	if err != nil || !ok || byKey.ID != jobID {
		t.Fatalf("%+v", byKey)
	}

	// Idempotency unique conflict
	job2 := domain.NewQueuedJob("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", designID, "idem-a", "fake", 2, now, "", d.SourceAssetID, d.TemplateVersion, digest)
	d2 := d
	d2.GenerationAttempts = 2
	d2.LatestJobID = job2.ID
	if err := jobs.BeginGenerating(ctx, job2, d2, now.Add(domain.SourceAttachedRetention), d2.GenerationAttempts-1, ""); !errors.Is(err, ports.ErrGenerationJobConflict) {
		t.Fatalf("idempotency conflict: %v", err)
	}

	// Active mutex conflict (different key)
	job3 := domain.NewQueuedJob("ffffffff-ffff-4fff-8fff-ffffffffffff", designID, "idem-b", "fake", 2, now, "", d.SourceAssetID, d.TemplateVersion, digest)
	if err := jobs.BeginGenerating(ctx, job3, d2, now.Add(domain.SourceAttachedRetention), d2.GenerationAttempts-1, ""); !errors.Is(err, ports.ErrGenerationJobConflict) {
		t.Fatalf("active mutex: %v", err)
	}

	active, ok, err := jobs.GetActiveByDesign(ctx, designID)
	if err != nil || !ok || active.ID != jobID {
		t.Fatalf("%+v", active)
	}
}

func TestInvalidateClaimExec_BumpsActive(t *testing.T) {
	db := testutil.OpenIsolated(t, "invalidate_claim")
	assets := postgres.NewAssetRepo(db)
	designs := postgres.NewDesignRepo(db)
	jobs := postgres.NewGenerationJobRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	designID := "bbbbbbbb-bbbb-4ccc-8ddd-bbbbbbbbbbbb"
	assetID := "aaaaaaaa-bbbb-4ccc-8ddd-aaaaaaaaaaaa"
	seedDesignForJobs(t, designs, assets, designID, assetID, now)
	d, _, _ := designs.GetByID(ctx, designID)
	jobID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	_ = d.RequestGeneration(jobID, 3, now)
	job := domain.NewQueuedJob(jobID, designID, "idem-inv", "fake", 1, now, "", d.SourceAssetID, d.TemplateVersion, domain.TextsDigestFor(d.Texts))
	if err := jobs.BeginGenerating(ctx, job, d, now.Add(domain.SourceAttachedRetention), d.GenerationAttempts-1, ""); err != nil {
		t.Fatal(err)
	}
	claimed, err := jobs.ClaimForWork(ctx, jobID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tokenBefore := claimed.ClaimToken
	if err := postgres.InvalidateClaimExec(ctx, db, jobID); err != nil {
		t.Fatal(err)
	}
	after, _, _ := jobs.GetByID(ctx, jobID)
	if after.ClaimToken != tokenBefore+1 || after.StartedAt != nil {
		t.Fatalf("after invalidate: %+v", after)
	}
	if err := postgres.InvalidateClaimExec(ctx, db, "00000000-0000-4000-8000-000000000099"); err != nil {
		t.Fatalf("missing product row: %v", err)
	}
}

type zeroRowsResult struct{}

func (zeroRowsResult) LastInsertId() (int64, error) { return 0, nil }
func (zeroRowsResult) RowsAffected() (int64, error) { return 0, nil }

type zeroRowsActiveExec struct {
	db *sql.DB
}

func (z *zeroRowsActiveExec) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return zeroRowsResult{}, nil
}
func (z *zeroRowsActiveExec) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return z.db.QueryRowContext(ctx, query, args...)
}

func TestInvalidateClaimExec_ActiveUnmatchedErrors(t *testing.T) {
	db := testutil.OpenIsolated(t, "invalidate_claim_unmatched")
	assets := postgres.NewAssetRepo(db)
	designs := postgres.NewDesignRepo(db)
	jobs := postgres.NewGenerationJobRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	designID := "bbbbbbbb-bbbb-4ccc-8ddd-bbbbbbbbbbbb"
	assetID := "aaaaaaaa-bbbb-4ccc-8ddd-aaaaaaaaaaaa"
	seedDesignForJobs(t, designs, assets, designID, assetID, now)
	d, _, _ := designs.GetByID(ctx, designID)
	jobID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	_ = d.RequestGeneration(jobID, 3, now)
	job := domain.NewQueuedJob(jobID, designID, "idem-unmatched", "fake", 1, now, "", d.SourceAssetID, d.TemplateVersion, domain.TextsDigestFor(d.Texts))
	if err := jobs.BeginGenerating(ctx, job, d, now.Add(domain.SourceAttachedRetention), d.GenerationAttempts-1, ""); err != nil {
		t.Fatal(err)
	}

	err := postgres.InvalidateClaimExec(ctx, &zeroRowsActiveExec{db: db}, jobID)
	if err == nil || !strings.Contains(err.Error(), "unmatched") {
		t.Fatalf("want unmatched active error, got %v", err)
	}
}

func TestGenerationJobRepo_PersistArtworkAndCompleteSuccessMockup(t *testing.T) {
	db := testutil.OpenIsolated(t, "generation_complete_mockup")
	assets := postgres.NewAssetRepo(db)
	designs := postgres.NewDesignRepo(db)
	jobs := postgres.NewGenerationJobRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	designID := "bbbbbbbb-bbbb-4ccc-8ddd-bbbbbbbbbbbb"
	assetID := "aaaaaaaa-bbbb-4ccc-8ddd-aaaaaaaaaaaa"
	seedDesignForJobs(t, designs, assets, designID, assetID, now)
	d, _, _ := designs.GetByID(ctx, designID)
	jobID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	_ = d.RequestGeneration(jobID, 3, now)
	job := domain.NewQueuedJob(jobID, designID, "idem-mock", "fake", 1, now, "", d.SourceAssetID, d.TemplateVersion, domain.TextsDigestFor(d.Texts))
	if err := jobs.BeginGenerating(ctx, job, d, now.Add(domain.SourceAttachedRetention), d.GenerationAttempts-1, ""); err != nil {
		t.Fatal(err)
	}
	claimed, err := jobs.ClaimForWork(ctx, jobID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	art1 := domain.Asset{
		ID: "11111111-1111-4111-8111-111111111111", Type: domain.AssetTypeArtwork,
		StorageKey: "private/blobs/metaldogs/artwork/2026/11/a1.png",
		MimeType:   "image/png", SizeBytes: 10, Width: 64, Height: 64,
		Checksum: application.ChecksumSHA256Prefix + "aa", CreatedAt: now, DeleteAfter: now.Add(time.Hour),
		OwnerRef: designID,
	}
	priorArt, err := jobs.PersistArtwork(ctx, claimed, art1)
	if err != nil {
		t.Fatal(err)
	}
	if len(priorArt) != 0 {
		t.Fatalf("first artwork prior=%v", priorArt)
	}
	got, _, _ := jobs.GetByID(ctx, jobID)
	if got.ArtworkAssetID != art1.ID || got.Status != domain.JobRunning {
		t.Fatalf("after persist: %+v", got)
	}

	art2 := art1
	art2.ID = "22222222-2222-4222-8222-222222222222"
	art2.StorageKey = "private/blobs/metaldogs/artwork/2026/11/a2.png"
	art2.Checksum = application.ChecksumSHA256Prefix + "bb"
	priorArt, err = jobs.PersistArtwork(ctx, claimed, art2)
	if err != nil {
		t.Fatal(err)
	}
	if len(priorArt) != 1 || priorArt[0].AssetID != art1.ID || priorArt[0].StorageKey != art1.StorageKey {
		t.Fatalf("replaced artwork prior=%+v", priorArt)
	}
	if _, ok, _ := assets.GetByID(ctx, art1.ID); ok {
		t.Fatal("prior artwork must be soft-deleted")
	}
	if live, ok, _ := assets.GetByID(ctx, art2.ID); !ok || live.Type != domain.AssetTypeArtwork {
		t.Fatalf("live artwork missing: ok=%v %+v", ok, live)
	}

	mock1 := domain.Asset{
		ID: "33333333-3333-4333-8333-333333333333", Type: domain.AssetTypeMockup,
		StorageKey: "private/blobs/metaldogs/mockup/2026/11/m1.png",
		MimeType:   "image/png", SizeBytes: 10, Width: 80, Height: 100,
		Checksum: application.ChecksumSHA256Prefix + "cc", CreatedAt: now, DeleteAfter: now.Add(time.Hour),
		OwnerRef: designID,
	}
	cost := 0.0
	claimed.ArtworkAssetID = art2.ID
	if err := claimed.Succeed(art2.ID, mock1.ID, []byte(`{"status":"pass"}`), 1, &cost, "USD", "fake", "v1", []byte(`{}`), now); err != nil {
		t.Fatal(err)
	}
	d.Status = domain.DesignPreviewReady
	d.UpdatedAt = now
	priorMock, err := jobs.CompleteSuccess(ctx, claimed, mock1, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(priorMock) != 0 {
		t.Fatalf("first mockup prior=%v", priorMock)
	}

	// Second success path via FinalizeSuccess replaces mockup under unique (owner_ref, type).
	d2, _, _ := designs.GetByID(ctx, designID)
	if d2.Status != domain.DesignPreviewReady {
		t.Fatalf("design status=%s", d2.Status)
	}
	job2ID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	priorAttempts := d2.GenerationAttempts
	priorLatest := d2.LatestJobID
	if priorLatest != jobID {
		t.Fatalf("expected latest_job_id=%s got %s", jobID, priorLatest)
	}
	_ = d2.RequestGeneration(job2ID, 3, now.Add(time.Second))
	job2 := domain.NewQueuedJob(job2ID, designID, "idem-mock-2", "fake", 2, now.Add(time.Second), "", d2.SourceAssetID, d2.TemplateVersion, domain.TextsDigestFor(d2.Texts))
	if err := jobs.BeginGenerating(ctx, job2, d2, now.Add(domain.SourceAttachedRetention), priorAttempts, priorLatest); err != nil {
		t.Fatal(err)
	}
	claimed2, err := jobs.ClaimForWork(ctx, job2ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	art3 := art2
	art3.ID = "44444444-4444-4444-8444-444444444444"
	art3.StorageKey = "private/blobs/metaldogs/artwork/2026/11/a3.png"
	art3.Checksum = application.ChecksumSHA256Prefix + "dd"
	if _, err := jobs.PersistArtwork(ctx, claimed2, art3); err != nil {
		t.Fatal(err)
	}
	mock2 := mock1
	mock2.ID = "55555555-5555-4555-8555-555555555555"
	mock2.StorageKey = "private/blobs/metaldogs/mockup/2026/11/m2.png"
	mock2.Checksum = application.ChecksumSHA256Prefix + "ee"
	claimed2.ArtworkAssetID = art3.ID
	if err := claimed2.Succeed(art3.ID, mock2.ID, []byte(`{"status":"pass"}`), 1, &cost, "USD", "fake", "v1", []byte(`{}`), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	d2.Status = domain.DesignPreviewReady
	d2.UpdatedAt = now.Add(time.Second)
	priorMock, err = jobs.FinalizeSuccess(ctx, claimed2, d2, mock2)
	if err != nil {
		t.Fatal(err)
	}
	if len(priorMock) != 1 || priorMock[0].AssetID != mock1.ID {
		t.Fatalf("replaced mockup prior=%+v", priorMock)
	}
	if _, ok, _ := assets.GetByID(ctx, mock1.ID); ok {
		t.Fatal("prior mockup must be soft-deleted")
	}
	liveMock, ok, _ := assets.GetByID(ctx, mock2.ID)
	if !ok || liveMock.Type != domain.AssetTypeMockup || liveMock.OwnerRef != designID {
		t.Fatalf("live mockup: ok=%v %+v", ok, liveMock)
	}
	// Exactly one live artwork and one live mockup for the design.
	var artCount, mockCount int
	err = db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE type = 'artwork' AND deleted_at IS NULL),
			COUNT(*) FILTER (WHERE type = 'mockup' AND deleted_at IS NULL)
		FROM metaldogs_assets WHERE owner_ref = $1
	`, designID).Scan(&artCount, &mockCount)
	if err != nil {
		t.Fatal(err)
	}
	if artCount != 1 || mockCount != 1 {
		t.Fatalf("live counts artwork=%d mockup=%d", artCount, mockCount)
	}
}

func TestGenerationJobRepo_BeginGeneratingStaleLatestFence(t *testing.T) {
	db := testutil.OpenIsolated(t, "generation_jobs_stale_latest")
	assets := postgres.NewAssetRepo(db)
	designs := postgres.NewDesignRepo(db)
	jobs := postgres.NewGenerationJobRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	designID := "bbbbbbbb-bbbb-4ccc-8ddd-bbbbbbbbbbbb"
	assetID := "aaaaaaaa-bbbb-4ccc-8ddd-aaaaaaaaaaaa"
	seedDesignForJobs(t, designs, assets, designID, assetID, now)
	d, _, _ := designs.GetByID(ctx, designID)
	jobID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	priorAttempts := d.GenerationAttempts
	priorLatest := d.LatestJobID
	_ = d.RequestGeneration(jobID, 3, now)
	job := domain.NewQueuedJob(jobID, designID, "idem-fence-1", "fake", 1, now, "", d.SourceAssetID, d.TemplateVersion, domain.TextsDigestFor(d.Texts))
	if err := jobs.BeginGenerating(ctx, job, d, now.Add(domain.SourceAttachedRetention), priorAttempts, priorLatest); err != nil {
		t.Fatal(err)
	}

	// Terminalize so status is eligible again, then attempt a second generate with a stale prior latest.
	claimed, err := jobs.ClaimForWork(ctx, jobID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	finished := now.Add(time.Minute)
	if err := claimed.Fail(domain.TerminalProviderError, "provider_error", finished); err != nil {
		t.Fatal(err)
	}
	if err := jobs.CompleteFailure(ctx, claimed, domain.Design{
		ID: designID, Status: domain.DesignGenerationFailed, UpdatedAt: finished,
	}); err != nil {
		t.Fatal(err)
	}
	d2, _, _ := designs.GetByID(ctx, designID)
	if d2.Status != domain.DesignGenerationFailed || d2.LatestJobID != jobID {
		t.Fatalf("after fail: %+v", d2)
	}
	job2ID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	stalePriorAttempts := d2.GenerationAttempts
	_ = d2.RequestGeneration(job2ID, 3, finished.Add(time.Second))
	job2 := domain.NewQueuedJob(job2ID, designID, "idem-fence-2", "fake", d2.GenerationAttempts, finished.Add(time.Second), "", d2.SourceAssetID, d2.TemplateVersion, domain.TextsDigestFor(d2.Texts))
	if err := jobs.BeginGenerating(ctx, job2, d2, finished.Add(domain.SourceAttachedRetention), stalePriorAttempts, "ffffffff-ffff-4fff-8fff-ffffffffffff"); !errors.Is(err, ports.ErrDesignConflict) {
		t.Fatalf("want ErrDesignConflict for stale latest, got %v", err)
	}
	if _, ok, _ := jobs.GetByID(ctx, job2ID); ok {
		t.Fatal("stale fence must not leave a new job row")
	}
	cur, _, _ := designs.GetByID(ctx, designID)
	if cur.LatestJobID != jobID || cur.GenerationAttempts != 1 {
		t.Fatalf("design must stay on first job: %+v", cur)
	}
}
