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
	if err := jobs.BeginGenerating(ctx, job, d, now.Add(domain.SourceAttachedRetention)); err != nil {
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
	if err := jobs.BeginGenerating(ctx, job2, d2, now.Add(domain.SourceAttachedRetention)); !errors.Is(err, ports.ErrGenerationJobConflict) {
		t.Fatalf("idempotency conflict: %v", err)
	}

	// Active mutex conflict (different key)
	job3 := domain.NewQueuedJob("ffffffff-ffff-4fff-8fff-ffffffffffff", designID, "idem-b", "fake", 2, now, "", d.SourceAssetID, d.TemplateVersion, digest)
	if err := jobs.BeginGenerating(ctx, job3, d2, now.Add(domain.SourceAttachedRetention)); !errors.Is(err, ports.ErrGenerationJobConflict) {
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
	if err := jobs.BeginGenerating(ctx, job, d, now.Add(domain.SourceAttachedRetention)); err != nil {
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
	if err := jobs.BeginGenerating(ctx, job, d, now.Add(domain.SourceAttachedRetention)); err != nil {
		t.Fatal(err)
	}

	err := postgres.InvalidateClaimExec(ctx, &zeroRowsActiveExec{db: db}, jobID)
	if err == nil || !strings.Contains(err.Error(), "unmatched") {
		t.Fatalf("want unmatched active error, got %v", err)
	}
}
