package metaldogs_test

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestGenerateFence_MissingTxBlocksOnlyGenerationRow(t *testing.T) {
	db := testutil.OpenIsolated(t, "fencenotx")
	ctx := context.Background()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	assetID := "aaaaaaaa-bbbb-4ccc-8ddd-aaaaaaaaaa01"
	designID := "bbbbbbbb-bbbb-4ccc-8ddd-bbbbbbbbbb01"
	jobID := "dddddddd-dddd-4ddd-8ddd-dddddddddd01"
	otherID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeee01"

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
	sum := sha256.Sum256([]byte("token-hash-bytes-for-fence-notx!!!!"))
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
	product := domain.NewQueuedJob(jobID, designID, "idem-fence-notx", "fake", stored.GenerationAttempts, now, "", stored.SourceAssetID, stored.TemplateVersion, domain.TextsDigestFor(stored.Texts))
	if err := genJobs.BeginGenerating(ctx, product, stored, now.Add(domain.SourceAttachedRetention), stored.GenerationAttempts-1, ""); err != nil {
		t.Fatal(err)
	}

	app := &plugin.App{Bootstrap: &plugin.Bootstrap{DB: db}}
	h := pluginsdk.AsHostFor(app, metaldogs.PluginName)
	h.Require(pluginsdk.CapDB)
	if err := metaldogs.InstallGenerateJobFence(h); err != nil {
		t.Fatal(err)
	}
	wrapped, err := app.ApplyQueueBoundHooks(&fenceHookStub{})
	if err != nil {
		t.Fatal(err)
	}
	bound := app.WrapRequeueNotify(wrapped)

	if err := bound.Requeue(ctx, otherID, 1); err != nil {
		t.Fatalf("requeue of a non-generation job: %v", err)
	}
	if err := bound.Requeue(ctx, "not-a-uuid", 1); err != nil {
		t.Fatalf("requeue of a non-uuid job id: %v", err)
	}
	err = bound.Requeue(ctx, jobID, 1)
	if err == nil || !strings.Contains(err.Error(), "requeue transaction missing") {
		t.Fatalf("generation row without reclaim tx: %v", err)
	}
}
