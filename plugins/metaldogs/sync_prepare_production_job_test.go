package metaldogs_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	infraPostgres "github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/production"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

// testPrintSpec is a fixture size, not a printer specification.
func testPrintSpec(w, h int, policy string, transparency bool) production.Spec {
	return production.Spec{
		WidthPx: w, HeightPx: h, DPI: 72,
		RequireTransparency: transparency,
		ColorProfile:        production.ProfileSRGB,
		UpscalePolicy:       policy,
	}
}

func TestPrepareProductionJob(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "prepare_production_job")
	ctx := context.Background()
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{Plugins: config.PluginsConfig{Extra: map[string]map[string]string{
			"metaldogs": {
				"enabled":                              "true",
				"production_spec.width_px":             "32",
				"production_spec.height_px":            "32",
				"production_spec.dpi":                  "72",
				"production_spec.require_transparency": "false",
				"production_spec.color_profile":        "srgb",
				"production_spec.upscale_policy":       "reject",
			},
		}}},
		Bootstrap: &plugin.Bootstrap{DB: db},
	}
	reg := plugin.NewRegistry(log)
	p := metaldogs.New()
	reg.Register(pluginsdk.Adapt(p))
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll %+v", summary)
	}
	var handler extapi.SyncJobHandler
	var maxRetries int
	for _, sj := range app.SyncJobs() {
		if sj.Job.Name == "prepare_production_artwork" {
			handler = sj.Job.Handler
			maxRetries = sj.Job.MaxRetries
		}
	}
	if handler == nil || maxRetries != 3 {
		t.Fatalf("handler nil=%v maxRetries=%d", handler == nil, maxRetries)
	}
	clock := testutil.NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	p.SetSnapshotClock(clock)
	queue, err := infraPostgres.NewJobQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	snaps := postgres.NewOrderDesignSnapshotRepo(db)

	t.Run("ready fills snapshot", func(t *testing.T) {
		seed := seedPrepareDesign(t, db)
		store := newMemBlobs()
		raw := solidPNG(t, 64, 64)
		store.put(seed.artworkKey, raw)
		p.SetAssetStore(store)
		orderID, variantID := insertOpenSnapshot(t, db, seed)
		buf := &bytes.Buffer{}
		if err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(buf, "info")); err != nil {
			t.Fatal(err)
		}
		assertNoSecrets(t, buf.String(), append([]string{seed.artworkKey}, store.keys()...)...)
		got, ok, err := postgres.NewDesignVersionRepo(db).GetByID(ctx, seed.versionID)
		if err != nil || !ok || got.ProductionStatus != domain.ProductionReady || got.ProductionFailureReason != "" {
			t.Fatalf("%+v ok=%v err=%v", got, ok, err)
		}
		if got.ArtworkChecksum != seed.artworkChecksum || got.Provider != "fake" {
			t.Fatalf("frozen fields changed: %+v", got)
		}
		wantID, err := application.ProductionAssetID(seed.versionID)
		if err != nil || got.ProductionAssetID != wantID {
			t.Fatalf("asset id %s want %s err %v", got.ProductionAssetID, wantID, err)
		}
		parsed, err := uuid.Parse(got.ProductionAssetID)
		if err != nil || parsed.Version() != 5 {
			t.Fatalf("asset id %s", got.ProductionAssetID)
		}
		art, ok, err := postgres.NewAssetRepo(db).GetByID(ctx, got.ProductionAssetID)
		if err != nil || !ok || art.Checksum != got.ProductionChecksum || art.Width != 32 || art.Height != 32 || art.Type != domain.AssetTypeProduction {
			t.Fatalf("%+v ok=%v err=%v", art, ok, err)
		}
		blob, ok := store.blob(art.StorageKey)
		if !ok {
			t.Fatal("production blob missing")
		}
		sum := sha256.Sum256(blob)
		if got.ProductionChecksum != "sha256:"+hex.EncodeToString(sum[:]) {
			t.Fatalf("checksum %s", got.ProductionChecksum)
		}
		img, err := png.Decode(bytes.NewReader(blob))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 32 || img.Bounds().Dy() != 32 {
			t.Fatalf("size %v", img.Bounds())
		}
		snap, ok, err := snaps.Get(ctx, orderID, variantID)
		if err != nil || !ok || snap.ProductionAssetID != got.ProductionAssetID || snap.ProductionChecksum != got.ProductionChecksum {
			t.Fatalf("%+v ok=%v err=%v", snap, ok, err)
		}
		puts := store.puts
		if err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(io.Discard, "error")); err != nil {
			t.Fatal(err)
		}
		if store.puts != puts {
			t.Fatalf("rerun puts %d want %d", store.puts, puts)
		}
		again, _, err := postgres.NewDesignVersionRepo(db).GetByID(ctx, seed.versionID)
		if err != nil || again.ProductionChecksum != got.ProductionChecksum || again.ProductionAssetID != got.ProductionAssetID {
			t.Fatalf("%+v err=%v", again, err)
		}
		if n := countProduction(t, db, seed.designID); n != 1 {
			t.Fatalf("assets=%d", n)
		}
	})

	t.Run("too small reject fails the job", func(t *testing.T) {
		seed := seedPrepareDesign(t, db)
		store := newMemBlobs()
		store.put(seed.artworkKey, solidPNG(t, 8, 8))
		p.SetAssetStore(store)
		p.SetProductionSpec(specPtr(testPrintSpec(32, 32, production.PolicyReject, false)))
		insertProcessingPrepareJob(t, db, seed.versionID)
		buf := &bytes.Buffer{}
		err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(buf, "info"))
		if err == nil || !strings.Contains(err.Error(), production.CodeTooSmall) {
			t.Fatalf("err=%v", err)
		}
		assertNoSecrets(t, buf.String(), append([]string{seed.artworkKey}, store.keys()...)...)
		assertVersion(t, db, seed.versionID, domain.ProductionFailed, production.CodeTooSmall)
		if store.puts != 0 || countProduction(t, db, seed.designID) != 0 {
			t.Fatalf("puts=%d", store.puts)
		}
		var maxRetries, attempts int
		if err := db.QueryRowContext(ctx, `SELECT max_retries, attempts FROM jobs WHERE id = $1`, seed.versionID).Scan(&maxRetries, &attempts); err != nil {
			t.Fatal(err)
		}
		if maxRetries != attempts || attempts != 1 {
			t.Fatalf("max_retries=%d attempts=%d", maxRetries, attempts)
		}
		if err := queue.Fail(ctx, seed.versionID, attempts, err); err != nil {
			t.Fatal(err)
		}
		var status, lastError string
		if err := db.QueryRowContext(ctx, `SELECT status, last_error FROM jobs WHERE id = $1`, seed.versionID).Scan(&status, &lastError); err != nil {
			t.Fatal(err)
		}
		if status != "failed" || !strings.Contains(lastError, production.CodeTooSmall) {
			t.Fatalf("status=%s last_error=%s", status, lastError)
		}
	})

	t.Run("upscale reaches spec size", func(t *testing.T) {
		seed := seedPrepareDesign(t, db)
		store := newMemBlobs()
		store.put(seed.artworkKey, solidPNG(t, 8, 8))
		p.SetAssetStore(store)
		p.SetProductionSpec(specPtr(testPrintSpec(16, 16, production.PolicyUpscale, false)))
		if err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(io.Discard, "error")); err != nil {
			t.Fatal(err)
		}
		got := mustVersion(t, db, seed.versionID)
		if got.ProductionStatus != domain.ProductionReady {
			t.Fatalf("%+v", got)
		}
		art, ok, err := postgres.NewAssetRepo(db).GetByID(ctx, got.ProductionAssetID)
		if err != nil || !ok || art.Width != 16 || art.Height != 16 {
			t.Fatalf("%+v ok=%v err=%v", art, ok, err)
		}
		blob, ok := store.blob(art.StorageKey)
		if !ok {
			t.Fatal("missing blob")
		}
		img, err := png.Decode(bytes.NewReader(blob))
		if err != nil || img.Bounds().Dx() != 16 || img.Bounds().Dy() != 16 {
			t.Fatalf("bounds %v err %v", img, err)
		}
	})

	t.Run("aspect mismatch", func(t *testing.T) {
		seed := seedPrepareDesign(t, db)
		store := newMemBlobs()
		store.put(seed.artworkKey, solidPNG(t, 30, 10))
		p.SetAssetStore(store)
		p.SetProductionSpec(specPtr(testPrintSpec(32, 32, production.PolicyReject, false)))
		err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(io.Discard, "error"))
		if err == nil || !strings.Contains(err.Error(), production.CodeAspectMismatch) {
			t.Fatalf("err=%v", err)
		}
		assertVersion(t, db, seed.versionID, domain.ProductionFailed, production.CodeAspectMismatch)
	})

	t.Run("transparency required", func(t *testing.T) {
		seed := seedPrepareDesign(t, db)
		store := newMemBlobs()
		store.put(seed.artworkKey, opaquePalettePNG(t, 16, 16))
		p.SetAssetStore(store)
		p.SetProductionSpec(specPtr(testPrintSpec(16, 16, production.PolicyReject, true)))
		err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(io.Discard, "error"))
		if err == nil || !strings.Contains(err.Error(), production.CodeTransparencyRequired) {
			t.Fatalf("err=%v", err)
		}
		assertVersion(t, db, seed.versionID, domain.ProductionFailed, production.CodeTransparencyRequired)
	})

	t.Run("artwork unreadable", func(t *testing.T) {
		seed := seedPrepareDesign(t, db)
		store := newMemBlobs()
		store.put(seed.artworkKey, []byte("not-a-png"))
		p.SetAssetStore(store)
		p.SetProductionSpec(specPtr(testPrintSpec(16, 16, production.PolicyReject, false)))
		err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(io.Discard, "error"))
		if err == nil || !strings.Contains(err.Error(), production.CodeArtworkUnreadable) {
			t.Fatalf("err=%v", err)
		}
		assertVersion(t, db, seed.versionID, domain.ProductionFailed, production.CodeArtworkUnreadable)
	})

	t.Run("invalid spec is permanent and leaves pending", func(t *testing.T) {
		seed := seedPrepareDesign(t, db)
		store := newMemBlobs()
		store.put(seed.artworkKey, solidPNG(t, 32, 32))
		p.SetAssetStore(store)
		p.SetProductionSpec(&production.Spec{WidthPx: 32, HeightPx: 32, DPI: 72, ColorProfile: "cmyk", UpscalePolicy: production.PolicyReject})
		insertProcessingPrepareJob(t, db, seed.versionID)
		err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(io.Discard, "error"))
		if err == nil || !strings.Contains(err.Error(), application.PrepareCodeMisconfigured) || strings.Contains(err.Error(), "database") {
			t.Fatalf("err=%v", err)
		}
		assertVersion(t, db, seed.versionID, domain.ProductionPending, "")
		assertTerminalized(t, ctx, db, seed.versionID)
	})

	t.Run("missing store is permanent and leaves pending", func(t *testing.T) {
		seed := seedPrepareDesign(t, db)
		p.SetAssetStore(nil)
		p.SetProductionSpec(specPtr(testPrintSpec(32, 32, production.PolicyReject, false)))
		insertProcessingPrepareJob(t, db, seed.versionID)
		err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(io.Discard, "error"))
		if err == nil || !strings.Contains(err.Error(), application.PrepareCodeMisconfigured) || strings.Contains(err.Error(), "database") {
			t.Fatalf("err=%v", err)
		}
		assertVersion(t, db, seed.versionID, domain.ProductionPending, "")
		assertTerminalized(t, ctx, db, seed.versionID)
	})

	t.Run("terminalize ignores another job type", func(t *testing.T) {
		seed := seedPrepareDesign(t, db)
		p.SetProductionSpec(nil)
		if _, err := db.ExecContext(ctx, `
			INSERT INTO jobs (id, type, payload, status, attempts, max_retries)
			VALUES ($1, 'integration.sync.other.prepare_production_artwork', '{}'::jsonb, 'processing', 1, 3)
		`, seed.versionID); err != nil {
			t.Fatal(err)
		}
		err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(io.Discard, "error"))
		if err == nil || !strings.Contains(err.Error(), "pending_spec") {
			t.Fatalf("err=%v", err)
		}
		var maxRetries int
		if err := db.QueryRowContext(ctx, `SELECT max_retries FROM jobs WHERE id = $1`, seed.versionID).Scan(&maxRetries); err != nil {
			t.Fatal(err)
		}
		if maxRetries != 3 {
			t.Fatalf("max_retries=%d", maxRetries)
		}
		assertVersion(t, db, seed.versionID, domain.ProductionPending, "")
	})

	t.Run("spec absent leaves pending", func(t *testing.T) {
		seed := seedPrepareDesign(t, db)
		store := newMemBlobs()
		store.put(seed.artworkKey, solidPNG(t, 32, 32))
		p.SetAssetStore(store)
		p.SetProductionSpec(nil)
		insertProcessingPrepareJob(t, db, seed.versionID)
		buf := &bytes.Buffer{}
		err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(buf, "info"))
		if err == nil || !strings.Contains(err.Error(), "pending_spec") {
			t.Fatalf("err=%v", err)
		}
		assertNoSecrets(t, buf.String(), seed.artworkKey)
		assertVersion(t, db, seed.versionID, domain.ProductionPending, "")
		if countProduction(t, db, seed.designID) != 0 {
			t.Fatal("spec absent wrote an asset")
		}
		if err := queue.Fail(ctx, seed.versionID, 1, err); err != nil {
			t.Fatal(err)
		}
		var status, lastError string
		if err := db.QueryRowContext(ctx, `SELECT status, last_error FROM jobs WHERE id = $1`, seed.versionID).Scan(&status, &lastError); err != nil {
			t.Fatal(err)
		}
		if status != "failed" || !strings.Contains(lastError, "pending_spec") {
			t.Fatalf("status=%s last_error=%s", status, lastError)
		}
	})

	t.Run("storage failure then retry", func(t *testing.T) {
		seed := seedPrepareDesign(t, db)
		store := newMemBlobs()
		store.failPuts = 1
		store.put(seed.artworkKey, solidPNG(t, 16, 16))
		p.SetAssetStore(store)
		p.SetProductionSpec(specPtr(testPrintSpec(16, 16, production.PolicyReject, false)))
		insertProcessingPrepareJob(t, db, seed.versionID)
		buf := &bytes.Buffer{}
		err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(buf, "info"))
		if err == nil || !strings.Contains(err.Error(), "storage") {
			t.Fatalf("err=%v", err)
		}
		assertNoSecrets(t, buf.String(), append([]string{seed.artworkKey}, store.keys()...)...)
		assertVersion(t, db, seed.versionID, domain.ProductionPreparing, "")
		if countProduction(t, db, seed.designID) != 0 || store.puts != 0 {
			t.Fatalf("puts=%d", store.puts)
		}
		var maxRetries int
		if err := db.QueryRowContext(ctx, `SELECT max_retries FROM jobs WHERE id = $1`, seed.versionID).Scan(&maxRetries); err != nil {
			t.Fatal(err)
		}
		if maxRetries != 3 {
			t.Fatalf("max_retries=%d", maxRetries)
		}
		if err := queue.Fail(ctx, seed.versionID, 1, err); err != nil {
			t.Fatal(err)
		}
		var status string
		if err := db.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = $1`, seed.versionID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "pending" {
			t.Fatalf("status=%s", status)
		}
		if err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(io.Discard, "error")); err != nil {
			t.Fatal(err)
		}
		assertVersion(t, db, seed.versionID, domain.ProductionReady, "")
		if store.puts != 1 {
			t.Fatalf("puts=%d", store.puts)
		}
	})

	t.Run("fill error does not fail the job", func(t *testing.T) {
		seed := seedPrepareDesign(t, db)
		store := newMemBlobs()
		store.put(seed.artworkKey, solidPNG(t, 16, 16))
		p.SetAssetStore(store)
		p.SetProductionSpec(specPtr(testPrintSpec(16, 16, production.PolicyReject, false)))
		p.SetProductionFill(func(context.Context, string) error {
			return errors.New("fill down")
		})
		t.Cleanup(func() { p.SetProductionFill(nil) })
		orderID, variantID := insertOpenSnapshot(t, db, seed)
		buf := &bytes.Buffer{}
		if err := runPrepare(ctx, handler, db, seed, logger.NewWithWriter(buf, "info")); err != nil {
			t.Fatal(err)
		}
		assertNoSecrets(t, buf.String(), append([]string{seed.artworkKey}, store.keys()...)...)
		if !strings.Contains(buf.String(), "fill_snapshot") || !strings.Contains(buf.String(), "fill down") {
			t.Fatalf("log %s", buf.String())
		}
		assertVersion(t, db, seed.versionID, domain.ProductionReady, "")
		snap, ok, err := snaps.Get(ctx, orderID, variantID)
		if err != nil || !ok || snap.ProductionAssetID != "" || snap.ProductionChecksum != "" {
			t.Fatalf("%+v ok=%v err=%v", snap, ok, err)
		}
	})
}

func specPtr(s production.Spec) *production.Spec { return &s }

type prepareSeed struct {
	designID        string
	versionID       string
	artworkID       string
	artworkKey      string
	artworkChecksum string
}

func seedPrepareDesign(t *testing.T, db *sql.DB) prepareSeed {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	assets := postgres.NewAssetRepo(db)
	designs := postgres.NewDesignRepo(db)
	versions := postgres.NewDesignVersionRepo(db)
	sourceID := uuid.NewString()
	designID := uuid.NewString()
	artID := uuid.NewString()
	verID := uuid.NewString()
	templateID := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	artKey := "private/blobs/metaldogs/artwork/2026/11/" + artID + ".png"
	if err := assets.Insert(ctx, domain.Asset{
		ID: sourceID, Type: domain.AssetTypeSource,
		StorageKey: "private/blobs/metaldogs/source/2026/11/" + sourceID + ".jpg",
		MimeType:   "image/jpeg", SizeBytes: 1, Width: 300, Height: 300,
		Checksum: "sha256:source", CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("owner-" + designID))
	d := domain.Design{
		ID: designID, TemplateID: templateID, TemplateVersion: 1,
		Status: domain.DesignDraft, Texts: map[string]string{"dog_name": "Burek"},
		SourceAssetID: sourceID, OwnerTokenHash: sum[:],
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(domain.DesignRetentionUnordered),
	}
	if err := designs.CreateBound(ctx, d, now.Add(domain.SourceAttachedRetention)); err != nil {
		t.Fatal(err)
	}
	checksum := "sha256:artwork-" + artID
	if err := assets.Insert(ctx, domain.Asset{
		ID: artID, Type: domain.AssetTypeArtwork, OwnerRef: designID,
		StorageKey: artKey, MimeType: "image/png", SizeBytes: 1, Width: 64, Height: 64,
		Checksum: checksum, CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE metaldogs_designs SET status = $1 WHERE id = $2`, domain.DesignPreviewReady, designID); err != nil {
		t.Fatal(err)
	}
	d.Status = domain.DesignPreviewReady
	if err := d.Approve(verID, now); err != nil {
		t.Fatal(err)
	}
	v, err := domain.NewDesignVersion(verID, designID, templateID, 1, d.Texts, artID, checksum, "fake", "fake", "pv1", []byte(`{}`), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := versions.ApproveBound(ctx, d, v); err != nil {
		t.Fatal(err)
	}
	return prepareSeed{
		designID: designID, versionID: verID, artworkID: artID,
		artworkKey: artKey, artworkChecksum: checksum,
	}
}

func insertOpenSnapshot(t *testing.T, db *sql.DB, seed prepareSeed) (string, string) {
	t.Helper()
	orderID := uuid.NewString()
	variantID := "line-m"
	snap, err := domain.NewOrderDesignSnapshot(
		orderID, variantID, seed.designID, seed.versionID, "TEE-M",
		"test-tee", 1, map[string]string{"dog_name": "Burek"},
		"fake", "fake", "pv1", seed.artworkID, seed.artworkChecksum,
		"", "", time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewOrderDesignSnapshotRepo(db).Insert(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	return orderID, variantID
}

func runPrepare(ctx context.Context, h extapi.SyncJobHandler, db *sql.DB, seed prepareSeed, log extapi.SyncLogger) error {
	return h(ctx, extapi.SyncJobContext{
		JobID:  seed.versionID,
		DB:     db,
		Logger: log,
		Payload: map[string]interface{}{
			"design_version_id": seed.versionID,
			"design_id":         seed.designID,
		},
	})
}

func insertProcessingPrepareJob(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO jobs (id, type, payload, status, attempts, max_retries)
		VALUES ($1, 'integration.sync.metaldogs.prepare_production_artwork', '{}'::jsonb, 'processing', 1, 3)
	`, id); err != nil {
		t.Fatal(err)
	}
}

func assertTerminalized(t *testing.T, ctx context.Context, db *sql.DB, id string) {
	t.Helper()
	var maxRetries, attempts int
	if err := db.QueryRowContext(ctx, `SELECT max_retries, attempts FROM jobs WHERE id = $1`, id).Scan(&maxRetries, &attempts); err != nil {
		t.Fatal(err)
	}
	if maxRetries != attempts || attempts != 1 {
		t.Fatalf("max_retries=%d attempts=%d", maxRetries, attempts)
	}
}

func assertVersion(t *testing.T, db *sql.DB, id, status, reason string) {
	t.Helper()
	got := mustVersion(t, db, id)
	if got.ProductionStatus != status || got.ProductionFailureReason != reason {
		t.Fatalf("status=%s reason=%s", got.ProductionStatus, got.ProductionFailureReason)
	}
}

func mustVersion(t *testing.T, db *sql.DB, id string) domain.DesignVersion {
	t.Helper()
	got, ok, err := postgres.NewDesignVersionRepo(db).GetByID(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	return got
}

func countProduction(t *testing.T, db *sql.DB, designID string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM metaldogs_assets WHERE owner_ref = $1 AND type = 'production' AND deleted_at IS NULL
	`, designID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func assertNoSecrets(t *testing.T, log string, keys ...string) {
	t.Helper()
	if strings.Contains(log, "private/blobs") || strings.Contains(log, "\x89PNG") {
		t.Fatalf("log contains storage key or png bytes: %s", log)
	}
	for _, key := range keys {
		if key != "" && strings.Contains(log, key) {
			t.Fatalf("log contains %s", key)
		}
	}
}

func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 180, G: 20, B: 20, A: 255})
		}
	}
	return encodeJobPNG(t, img)
}

func opaquePalettePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	pal := color.Palette{
		color.NRGBA{R: 255, G: 0, B: 0, A: 255},
		color.NRGBA{R: 0, G: 0, B: 0, A: 255},
	}
	img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	return encodeJobPNG(t, img)
}

func encodeJobPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type memBlobs struct {
	mu       sync.Mutex
	blobs    map[string][]byte
	failPuts int
	puts     int
}

func newMemBlobs() *memBlobs {
	return &memBlobs{blobs: map[string][]byte{}}
}

func (m *memBlobs) put(key string, b []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobs[key] = append([]byte(nil), b...)
}

func (m *memBlobs) blob(key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.blobs[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), b...), true
}

func (m *memBlobs) keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.blobs))
	for k := range m.blobs {
		out = append(out, k)
	}
	return out
}

func (m *memBlobs) Put(_ context.Context, key string, r io.Reader, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failPuts > 0 {
		m.failPuts--
		return errors.New("storage down")
	}
	if m.blobs == nil {
		m.blobs = map[string][]byte{}
	}
	m.blobs[key] = append([]byte(nil), b...)
	m.puts++
	return nil
}

func (m *memBlobs) Read(_ context.Context, key string) ([]byte, error) {
	b, ok := m.blob(key)
	if !ok {
		return nil, errors.New("missing")
	}
	return b, nil
}

func (m *memBlobs) Head(context.Context, string) error { return nil }
func (m *memBlobs) SignedGetURL(context.Context, string, time.Duration) (string, error) {
	return "", errors.New("no url")
}
func (m *memBlobs) Delete(context.Context, string) error { return nil }

var _ ports.AssetStore = (*memBlobs)(nil)
