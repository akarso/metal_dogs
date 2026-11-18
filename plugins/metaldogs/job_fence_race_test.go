package metaldogs_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	platformjobs "github.com/akarso/shopanda/internal/domain/jobs"
	infraPostgres "github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestGenerateFence_StaleAttemptAndRequeueSameTx(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "jobfence")
	ctx := context.Background()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

	assets := postgres.NewAssetRepo(db)
	designs := postgres.NewDesignRepo(db)
	genJobs := postgres.NewGenerationJobRepo(db)
	designID := "bbbbbbbb-bbbb-4ccc-8ddd-bbbbbbbbbbbb"
	assetID := "aaaaaaaa-bbbb-4ccc-8ddd-aaaaaaaaaaaa"
	jobID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	sum := sha256.Sum256([]byte("token-hash-bytes-for-fence-race!!!!"))
	if err := assets.Insert(ctx, domain.Asset{
		ID: assetID, Type: domain.AssetTypeSource,
		StorageKey: "private/blobs/metaldogs/source/2026/11/" + assetID + ".jpg",
		MimeType:   "image/jpeg", SizeBytes: 1, Width: 300, Height: 300,
		Checksum: application.ChecksumSHA256Prefix + "ab", CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
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
	product := domain.NewQueuedJob(jobID, designID, "idem-fence-race", "fake", stored.GenerationAttempts, now, "", stored.SourceAssetID, stored.TemplateVersion, domain.TextsDigestFor(stored.Texts))
	if err := genJobs.BeginGenerating(ctx, product, stored, now.Add(domain.SourceAttachedRetention), stored.GenerationAttempts-1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := genJobs.ClaimForWork(ctx, jobID, domain.DefaultClaimLease); err != nil {
		t.Fatal(err)
	}
	var claimBefore int
	if err := db.QueryRowContext(ctx, `SELECT claim_token FROM metaldogs_generation_jobs WHERE id = $1`, jobID).Scan(&claimBefore); err != nil {
		t.Fatal(err)
	}

	queue, err := infraPostgres.NewJobQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	platformJob, err := platformjobs.NewJob(jobID, "integration.sync.metaldogs.generate", map[string]interface{}{
		"generation_job_id": jobID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(ctx, platformJob); err != nil {
		t.Fatal(err)
	}
	dequeued, err := queue.Dequeue(ctx)
	if err != nil || dequeued == nil || dequeued.Attempts != 1 {
		t.Fatalf("dequeue: %+v err=%v", dequeued, err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE jobs SET updated_at = NOW() - interval '10 minutes' WHERE id = $1`, jobID); err != nil {
		t.Fatal(err)
	}

	app := &plugin.App{Bootstrap: &plugin.Bootstrap{DB: db}}
	h := pluginsdk.AsHostFor(app, metaldogs.PluginName)
	h.Require(pluginsdk.CapDB)
	if err := metaldogs.InstallGenerateJobFence(h); err != nil {
		t.Fatal(err)
	}
	probed := make(chan int, 1)
	release := make(chan struct{})
	var probedOnce bool
	app.OnRequeued(func(ctx context.Context, id string, attempt int) error {
		if probedOnce {
			return nil
		}
		probedOnce = true
		tx, ok := pluginsdk.RequeueTx(ctx)
		if !ok || tx == nil {
			return errors.New("requeue tx missing")
		}
		var status string
		var attempts int
		if err := tx.QueryRowContext(ctx, `SELECT status, attempts FROM jobs WHERE id = $1`, id).Scan(&status, &attempts); err != nil {
			return err
		}
		if status != string(platformjobs.StatusProcessing) || attempts != 1 || attempt != 1 {
			return errors.New("hook saw job " + status)
		}
		var outside int
		if err := db.QueryRowContext(ctx, `SELECT claim_token FROM metaldogs_generation_jobs WHERE id = $1`, id).Scan(&outside); err != nil {
			return err
		}
		probed <- outside
		<-release
		return nil
	})
	bound := app.WrapRequeueNotify(queue)

	errCh := make(chan error, 1)
	go func() {
		errCh <- bound.Requeue(ctx, jobID, 1, platformjobs.WithRequeueMinAge(time.Second))
	}()
	var outsideDuring int
	select {
	case err := <-errCh:
		t.Fatalf("requeue before hook: %v", err)
	case outsideDuring = <-probed:
	case <-time.After(5 * time.Second):
		t.Fatal("OnRequeued did not run")
	}
	if outsideDuring != claimBefore {
		t.Fatalf("claim visible outside reclaim tx: got %d want %d", outsideDuring, claimBefore)
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	var status string
	var attempts int
	if err := db.QueryRowContext(ctx, `SELECT status, attempts FROM jobs WHERE id = $1`, jobID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != string(platformjobs.StatusPending) || attempts != 1 {
		t.Fatalf("after requeue status=%s attempts=%d", status, attempts)
	}
	var claimAfterRequeue int
	var started interface{}
	if err := db.QueryRowContext(ctx, `SELECT claim_token, started_at FROM metaldogs_generation_jobs WHERE id = $1`, jobID).Scan(&claimAfterRequeue, &started); err != nil {
		t.Fatal(err)
	}
	if claimAfterRequeue != claimBefore+1 || started != nil {
		t.Fatalf("claim_token=%d started_at=%v want token %d and nil started_at", claimAfterRequeue, started, claimBefore+1)
	}

	second, err := queue.Dequeue(ctx)
	if err != nil || second == nil || second.Attempts != 2 {
		t.Fatalf("second dequeue: %+v err=%v", second, err)
	}
	if err := bound.Complete(ctx, jobID, 1); !errors.Is(err, platformjobs.ErrOutcomeNotApplied) {
		t.Fatalf("stale Complete: %v", err)
	}
	if err := bound.Fail(ctx, jobID, 1, errors.New("stale")); !errors.Is(err, platformjobs.ErrOutcomeNotApplied) {
		t.Fatalf("stale Fail: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status, attempts FROM jobs WHERE id = $1`, jobID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != string(platformjobs.StatusProcessing) || attempts != 2 {
		t.Fatalf("after stale outcome status=%s attempts=%d", status, attempts)
	}
	var claimAfter int
	if err := db.QueryRowContext(ctx, `SELECT claim_token FROM metaldogs_generation_jobs WHERE id = $1`, jobID).Scan(&claimAfter); err != nil {
		t.Fatal(err)
	}
	if claimAfter != claimAfterRequeue {
		t.Fatalf("stale outcome moved claim_token %d -> %d", claimAfterRequeue, claimAfter)
	}

	app.OnRequeued(func(context.Context, string, int) error {
		return errors.New("hook failed")
	})
	bound = app.WrapRequeueNotify(queue)
	if _, err := db.ExecContext(ctx, `
		UPDATE jobs SET updated_at = NOW() - interval '10 minutes' WHERE id = $1`, jobID); err != nil {
		t.Fatal(err)
	}
	err = bound.Requeue(ctx, jobID, 2, platformjobs.WithRequeueMinAge(time.Second))
	if err == nil || !strings.Contains(err.Error(), "hook failed") {
		t.Fatalf("hook error: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status, attempts FROM jobs WHERE id = $1`, jobID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != string(platformjobs.StatusProcessing) || attempts != 2 {
		t.Fatalf("after hook error status=%s attempts=%d", status, attempts)
	}
	if err := db.QueryRowContext(ctx, `SELECT claim_token FROM metaldogs_generation_jobs WHERE id = $1`, jobID).Scan(&claimAfter); err != nil {
		t.Fatal(err)
	}
	if claimAfter != claimAfterRequeue {
		t.Fatalf("hook error committed claim_token %d -> %d", claimAfterRequeue, claimAfter)
	}
}
