package metaldogs_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	infraPostgres "github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestEnqueueGenerate_RependTerminalKeepsAttemptsAndBumpsClaim(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "repend")
	ctx := context.Background()
	queue, err := infraPostgres.NewJobQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	app := testApp(t, true)
	app.Bootstrap = &plugin.Bootstrap{DB: db}
	reg := plugin.NewRegistry(app.Logger)
	p := metaldogs.New()
	reg.Register(p)
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll %+v", summary)
	}
	svc := p.GenerateService()
	if svc == nil || svc.Enqueue == nil {
		t.Fatal("generate enqueue not wired")
	}
	if err := app.BindQueue(app.WrapRequeueNotify(queue)); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		status string
		repend bool
	}{
		{status: "failed", repend: true},
		{status: "done", repend: true},
		{status: "cancelled", repend: true},
		{status: "pending", repend: false},
		{status: "processing", repend: false},
	}
	for i, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			jobID, designID := seedRunningGenerate(t, db, i+1)
			if _, err := db.ExecContext(ctx, `
				INSERT INTO jobs (id, type, payload, status, attempts, max_retries, last_error)
				VALUES ($1, 'integration.sync.metaldogs.generate', '{}'::jsonb, $2, 4, 3, 'boom')`,
				jobID, tc.status); err != nil {
				t.Fatal(err)
			}
			var claimBefore int
			var startedBefore sql.NullTime
			if err := db.QueryRowContext(ctx, `
				SELECT claim_token, started_at FROM metaldogs_generation_jobs WHERE id = $1`, jobID).
				Scan(&claimBefore, &startedBefore); err != nil {
				t.Fatal(err)
			}
			if !startedBefore.Valid {
				t.Fatal("seed must set started_at")
			}
			if err := svc.Enqueue(ctx, jobID, designID); err != nil {
				t.Fatal(err)
			}
			var status string
			var attempts int
			var lastError sql.NullString
			if err := db.QueryRowContext(ctx, `
				SELECT status, attempts, last_error FROM jobs WHERE id = $1`, jobID).
				Scan(&status, &attempts, &lastError); err != nil {
				t.Fatal(err)
			}
			var claimAfter int
			var startedAfter sql.NullTime
			if err := db.QueryRowContext(ctx, `
				SELECT claim_token, started_at FROM metaldogs_generation_jobs WHERE id = $1`, jobID).
				Scan(&claimAfter, &startedAfter); err != nil {
				t.Fatal(err)
			}
			if attempts != 4 || !lastError.Valid || lastError.String != "boom" {
				t.Fatalf("attempts=%d last_error=%v", attempts, lastError)
			}
			if tc.repend {
				if status != "pending" || claimAfter != claimBefore+1 || startedAfter.Valid {
					t.Fatalf("status=%s claim %d→%d started_valid=%v", status, claimBefore, claimAfter, startedAfter.Valid)
				}
				return
			}
			if status != tc.status || claimAfter != claimBefore || !startedAfter.Valid {
				t.Fatalf("status=%s claim %d→%d started_valid=%v", status, claimBefore, claimAfter, startedAfter.Valid)
			}
		})
	}
}

func TestEnqueueGenerate_RependTerminalHookErrorRollsBack(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "repend-hook")
	ctx := context.Background()
	queue, err := infraPostgres.NewJobQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	app := testApp(t, true)
	app.Bootstrap = &plugin.Bootstrap{DB: db}
	reg := plugin.NewRegistry(app.Logger)
	p := metaldogs.New()
	reg.Register(p)
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll %+v", summary)
	}
	app.OnRequeued(func(context.Context, string, int) error {
		return errors.New("hook failed")
	})
	if err := app.BindQueue(app.WrapRequeueNotify(queue)); err != nil {
		t.Fatal(err)
	}
	svc := p.GenerateService()
	if svc == nil || svc.Enqueue == nil {
		t.Fatal("generate enqueue not wired")
	}
	jobID, designID := seedRunningGenerate(t, db, 1)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO jobs (id, type, payload, status, attempts, max_retries, last_error)
		VALUES ($1, 'integration.sync.metaldogs.generate', '{}'::jsonb, 'failed', 4, 3, 'boom')`,
		jobID); err != nil {
		t.Fatal(err)
	}
	var claimBefore int
	if err := db.QueryRowContext(ctx, `
		SELECT claim_token FROM metaldogs_generation_jobs WHERE id = $1`, jobID).Scan(&claimBefore); err != nil {
		t.Fatal(err)
	}
	err = svc.Enqueue(ctx, jobID, designID)
	if err == nil || !strings.Contains(err.Error(), "hook failed") {
		t.Fatalf("enqueue: %v", err)
	}
	var status string
	var attempts int
	var lastError sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT status, attempts, last_error FROM jobs WHERE id = $1`, jobID).
		Scan(&status, &attempts, &lastError); err != nil {
		t.Fatal(err)
	}
	var claimAfter int
	if err := db.QueryRowContext(ctx, `
		SELECT claim_token FROM metaldogs_generation_jobs WHERE id = $1`, jobID).Scan(&claimAfter); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || attempts != 4 || !lastError.Valid || lastError.String != "boom" || claimAfter != claimBefore {
		t.Fatalf("status=%s attempts=%d last_error=%v claim %d→%d", status, attempts, lastError, claimBefore, claimAfter)
	}
}

func seedRunningGenerate(t *testing.T, db *sql.DB, n int) (jobID, designID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	assetID := fmt.Sprintf("10000000-0000-4000-8000-%012d", n)
	designID = fmt.Sprintf("20000000-0000-4000-8000-%012d", n)
	jobID = fmt.Sprintf("30000000-0000-4000-8000-%012d", n)
	assets := postgres.NewAssetRepo(db)
	designs := postgres.NewDesignRepo(db)
	genJobs := postgres.NewGenerationJobRepo(db)
	if err := assets.Insert(ctx, domain.Asset{
		ID: assetID, Type: domain.AssetTypeSource,
		StorageKey: "private/blobs/metaldogs/source/2026/11/" + assetID + ".jpg",
		MimeType:   "image/jpeg", SizeBytes: 1, Width: 300, Height: 300,
		Checksum: application.ChecksumSHA256Prefix + "ab", CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("token-hash-bytes-for-repend!!!!!!"))
	d := domain.Design{
		ID: designID, TemplateID: "cccccccc-cccc-4ccc-8ddd-cccccccccccc", TemplateVersion: 1,
		Status: domain.DesignDraft, Texts: map[string]string{"dog_name": "Burek"},
		SourceAssetID: assetID, OwnerTokenHash: sum[:],
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(domain.DesignRetentionUnordered),
	}
	if err := designs.CreateBound(ctx, d, now.Add(domain.SourceAttachedRetention)); err != nil {
		t.Fatal(err)
	}
	stored, ok, err := designs.GetByID(ctx, designID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := stored.RequestGeneration(jobID, 3, now); err != nil {
		t.Fatal(err)
	}
	product := domain.NewQueuedJob(jobID, designID, fmt.Sprintf("idem-repend-%d", n), "fake", stored.GenerationAttempts, now, "", stored.SourceAssetID, stored.TemplateVersion, domain.TextsDigestFor(stored.Texts))
	if err := genJobs.BeginGenerating(ctx, product, stored, now.Add(domain.SourceAttachedRetention), stored.GenerationAttempts-1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := genJobs.ClaimForWork(ctx, jobID, domain.DefaultClaimLease); err != nil {
		t.Fatal(err)
	}
	return jobID, designID
}
