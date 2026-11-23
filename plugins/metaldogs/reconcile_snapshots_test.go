package metaldogs_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/internal/application/integration"
	platformjobs "github.com/akarso/shopanda/internal/domain/jobs"
	infraPostgres "github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/memory"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestReconcileSnapshots_RegisteredOnlyWhenFlagOn(t *testing.T) {
	t.Parallel()
	off := testApp(t, true)
	regOff := plugin.NewRegistry(off.Logger)
	regOff.Register(pluginsdk.Adapt(metaldogs.New()))
	if summary := regOff.InitAll(off); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll %+v", summary)
	}
	cmd, ok := regOff.CLIRegistry().Get(metaldogs.CommandSnapshots)
	if !ok || !cmd.RequiresDB {
		t.Fatalf("cli %+v", cmd)
	}
	for _, sj := range off.SyncJobs() {
		if sj.Job.Name == metaldogs.ReconcileSnapshotsJobName {
			t.Fatalf("flag off registered %+v", sj)
		}
	}

	on := testApp(t, true)
	on.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	regOn := plugin.NewRegistry(on.Logger)
	regOn.Register(pluginsdk.Adapt(metaldogs.New()))
	if summary := regOn.InitAll(on); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll %+v", summary)
	}
	var found bool
	for _, sj := range on.SyncJobs() {
		if sj.Job.Name != metaldogs.ReconcileSnapshotsJobName {
			continue
		}
		found = true
		if sj.JobType != "integration.sync.metaldogs.reconcile_snapshots" {
			t.Fatalf("type %s", sj.JobType)
		}
		if sj.Job.Trigger.Kind != extapi.SyncTriggerCron || sj.Job.Trigger.CronSpec != "@every 5m" {
			t.Fatalf("trigger %+v", sj.Job.Trigger)
		}
	}
	if !found {
		t.Fatal("flag on did not register reconcile_snapshots")
	}
}

func TestReconcileSnapshots(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "reconcile_snapshots")
	ctx := context.Background()
	queue, err := infraPostgres.NewJobQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	app := testApp(t, true)
	app.Bootstrap = &plugin.Bootstrap{DB: db}
	app.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	reg := plugin.NewRegistry(app.Logger)
	p := metaldogs.New()
	reg.Register(pluginsdk.Adapt(p))
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll %+v", summary)
	}
	if err := bindRependQueue(t, app, queue); err != nil {
		t.Fatal(err)
	}
	src := memory.NewOrderSource()
	p.SetOrderSource(src)
	cat := &application.TemplateCatalog{Store: postgres.NewTemplateStore(db), IDs: ports.UUIDGenerator{}}
	snaps := postgres.NewOrderDesignSnapshotRepo(db)
	handler := findSnapshotHandler(t, app)
	created := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	missingSeed := seedApprovedDesign(t, db, cat, "tee-reconcile-missing", "prompt-reconcile-missing", false)
	missingOrder := uuid.NewString()
	missingVariant := uuid.NewString()
	otherVariant := uuid.NewString()
	src.Add(missingOrder, created, []ports.OrderLine{
		{VariantID: missingVariant, SKU: "TEE-M", Quantity: 1, DesignID: missingSeed.Design.ID},
		{VariantID: otherVariant, SKU: "TEE-PLAIN", Quantity: 1},
	})

	heldSeed := seedApprovedDesign(t, db, cat, "tee-reconcile-held", "prompt-reconcile-held", false)
	heldOrder := uuid.NewString()
	heldVariant := uuid.NewString()
	src.Add(heldOrder, created.Add(time.Second), []ports.OrderLine{{
		VariantID: heldVariant, SKU: "TEE-L", Quantity: 1, DesignID: heldSeed.Design.ID,
	}})
	if err := runSnapshot(t, handler, heldOrder, map[string]interface{}{"order_id": heldOrder}, &snapLog{}); err != nil {
		t.Fatal(err)
	}

	out, err := runReconcile(t, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantLine := "order_id=" + missingOrder + " line_key=" + missingOrder + "/" + missingVariant + " reason=" + application.ReconcileReasonMissing
	if !strings.Contains(out, wantLine) {
		t.Fatalf("report %q", out)
	}
	if strings.Contains(out, otherVariant) {
		t.Fatalf("plain line reported %q", out)
	}
	if strings.Contains(out, heldOrder) {
		t.Fatalf("snapshotted order reported %q", out)
	}
	if strings.Count(out, "order_id="+missingOrder) != 1 {
		t.Fatalf("report %q", out)
	}

	narrow, err := runReconcile(t, reg, []string{"--order=" + missingOrder})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(narrow, wantLine) || strings.Contains(narrow, heldOrder) {
		t.Fatalf("narrow %q", narrow)
	}

	if _, err := runReconcile(t, reg, []string{"--order=" + missingOrder, "--fix"}); err != nil {
		t.Fatal(err)
	}
	jobID := metaldogs.SnapshotOrderJobID(missingOrder)
	if n := countJobID(t, db, jobID); n != 1 {
		t.Fatalf("jobs=%d", n)
	}
	if _, err := runReconcile(t, reg, []string{"--fix", "--order=" + missingOrder}); err != nil {
		t.Fatal(err)
	}
	if n := countJobID(t, db, jobID); n != 1 {
		t.Fatalf("jobs after repeat=%d", n)
	}

	appOff := testApp(t, true)
	appOff.Bootstrap = &plugin.Bootstrap{DB: db}
	regOff := plugin.NewRegistry(appOff.Logger)
	pOff := metaldogs.New()
	regOff.Register(pluginsdk.Adapt(pOff))
	if summary := regOff.InitAll(appOff); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll %+v", summary)
	}
	srcOff := memory.NewOrderSource()
	refusedOrder := uuid.NewString()
	srcOff.Add(refusedOrder, created, []ports.OrderLine{{
		VariantID: uuid.NewString(), SKU: "TEE-M", Quantity: 1, DesignID: missingSeed.Design.ID,
	}})
	pOff.SetOrderSource(srcOff)
	refused, err := runReconcile(t, regOff, []string{"--order=" + refusedOrder, "--fix"})
	if err == nil || !strings.Contains(err.Error(), "--fix refused") || !strings.Contains(err.Error(), "order_snapshot.enabled") {
		t.Fatalf("err=%v out=%q", err, refused)
	}
	if !strings.Contains(refused, refusedOrder) || !strings.Contains(refused, application.ReconcileReasonMissing) {
		t.Fatalf("flag off did not report %q", refused)
	}
	if n := countJobID(t, db, metaldogs.SnapshotOrderJobID(refusedOrder)); n != 0 {
		t.Fatalf("flag off enqueued %d", n)
	}

	if _, err := db.ExecContext(ctx, `UPDATE jobs SET run_at = NOW() + interval '1 day' WHERE status = 'pending'`); err != nil {
		t.Fatal(err)
	}
	failedOrder := uuid.NewString()
	failedVariant := uuid.NewString()
	src.Add(failedOrder, created, []ports.OrderLine{{
		VariantID: failedVariant, SKU: "TEE-M", Quantity: 1, DesignID: uuid.NewString(),
	}})
	if err := p.EnqueueSnapshotOrder(ctx, failedOrder); err != nil {
		t.Fatal(err)
	}
	worker := platformjobs.NewWorker(queue, app.Logger, time.Millisecond)
	if err := integration.RegisterSyncJobHandlers(app, worker); err != nil {
		t.Fatal(err)
	}
	driveSnapshotUntilFailed(t, db, worker, metaldogs.SnapshotOrderJobID(failedOrder))
	failedOut, err := runReconcile(t, reg, []string{"--order=" + failedOrder})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(failedOut, "order_id="+failedOrder) || !strings.Contains(failedOut, failedOrder+"/"+failedVariant) ||
		!strings.Contains(failedOut, "reason=snapshot_job_failed:") || !strings.Contains(failedOut, "not found") {
		t.Fatalf("failed report %q", failedOut)
	}

	fillSeed := seedApprovedDesign(t, db, cat, "tee-reconcile-fill", "prompt-reconcile-fill", false)
	fillOrder := uuid.NewString()
	fillVariant := uuid.NewString()
	src.Add(fillOrder, created, []ports.OrderLine{{
		VariantID: fillVariant, SKU: "TEE-M", Quantity: 1, DesignID: fillSeed.Design.ID,
	}})
	if err := runSnapshot(t, handler, fillOrder, map[string]interface{}{"order_id": fillOrder}, &snapLog{}); err != nil {
		t.Fatal(err)
	}
	prodID := markProductionReady(t, db, fillSeed.Design.ID, fillSeed.Version.ID)
	if err := p.FillSnapshotProduction(ctx, fillSeed.Version.ID); err != nil {
		t.Fatal(err)
	}
	got, ok, err := snaps.Get(ctx, fillOrder, fillVariant)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if got.ProductionAssetID != prodID || got.ProductionChecksum != "sha256:late" {
		t.Fatalf("%+v", got)
	}
	if err := p.FillSnapshotProduction(ctx, fillSeed.Version.ID); err != nil {
		t.Fatal(err)
	}
	again, _, err := snaps.Get(ctx, fillOrder, fillVariant)
	if err != nil {
		t.Fatal(err)
	}
	if again.ProductionAssetID != prodID || again.ProductionChecksum != "sha256:late" {
		t.Fatalf("second fill %+v", again)
	}
	if err := snaps.FillProduction(ctx, fillOrder, fillVariant, uuid.NewString(), "sha256:other"); !errors.Is(err, domain.ErrProductionAlreadySet) {
		t.Fatalf("second write %v", err)
	}

	sweepSeed := seedApprovedDesign(t, db, cat, "tee-reconcile-sweep-fill", "prompt-reconcile-sweep-fill", false)
	sweepOrder := uuid.NewString()
	sweepVariant := uuid.NewString()
	src.Add(sweepOrder, created, []ports.OrderLine{{
		VariantID: sweepVariant, SKU: "TEE-S", Quantity: 1, DesignID: sweepSeed.Design.ID,
	}})
	if err := runSnapshot(t, handler, sweepOrder, map[string]interface{}{"order_id": sweepOrder}, &snapLog{}); err != nil {
		t.Fatal(err)
	}
	sweepProd := markProductionReady(t, db, sweepSeed.Design.ID, sweepSeed.Version.ID)
	sweepOut, err := runReconcile(t, reg, []string{"--order=" + sweepOrder})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sweepOut, sweepOrder) {
		t.Fatalf("filled order reported %q", sweepOut)
	}
	swept, ok, err := snaps.Get(ctx, sweepOrder, sweepVariant)
	if err != nil || !ok || swept.ProductionAssetID != sweepProd || swept.ProductionChecksum != "sha256:late" {
		t.Fatalf("ok=%v err=%v %+v", ok, err, swept)
	}

	cronOrder := uuid.NewString()
	cronVariant := uuid.NewString()
	src.Add(cronOrder, created, []ports.OrderLine{{
		VariantID: cronVariant, SKU: "TEE-M", Quantity: 1, DesignID: missingSeed.Design.ID,
	}})
	snapsBefore := countTable(t, db, `SELECT count(*) FROM metaldogs_order_design_snapshots`)
	jobsBefore := countTable(t, db, `SELECT count(*) FROM jobs WHERE type = 'integration.sync.metaldogs.snapshot_order_design'`)
	cron := findReconcileHandler(t, app)
	log := &snapLog{}
	if err := cron(ctx, extapi.SyncJobContext{DB: db, Logger: log, Name: metaldogs.ReconcileSnapshotsJobName}); err != nil {
		t.Fatal(err)
	}
	if countTable(t, db, `SELECT count(*) FROM metaldogs_order_design_snapshots`) != snapsBefore {
		t.Fatal("cron inserted a snapshot")
	}
	if countTable(t, db, `SELECT count(*) FROM jobs WHERE type = 'integration.sync.metaldogs.snapshot_order_design'`) != jobsBefore {
		t.Fatal("cron enqueued a snapshot job")
	}
	if !log.contains("metaldogs.reconcile_snapshots.gap") || !log.contains(cronOrder) || !log.contains(cronOrder+"/"+cronVariant) || !log.contains(application.ReconcileReasonMissing) {
		t.Fatalf("cron log %+v", log.lines)
	}
}

func runReconcile(t *testing.T, reg *plugin.Registry, args []string) (string, error) {
	t.Helper()
	cmd, ok := reg.CLIRegistry().Get(metaldogs.CommandSnapshots)
	if !ok {
		t.Fatal("metaldogs:snapshots not registered")
	}
	var buf bytes.Buffer
	metaldogs.SetReconcileOutput(&buf)
	defer metaldogs.SetReconcileOutput(nil)
	err := cmd.Run(cli.Context{Ctx: context.Background()}, append([]string{"reconcile"}, args...))
	return buf.String(), err
}

func findReconcileHandler(t *testing.T, app *plugin.App) extapi.SyncJobHandler {
	t.Helper()
	for _, sj := range app.SyncJobs() {
		if sj.Job.Name == metaldogs.ReconcileSnapshotsJobName {
			return sj.Job.Handler
		}
	}
	t.Fatal("reconcile handler missing")
	return nil
}

func countJobID(t *testing.T, db *sql.DB, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM jobs WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countTable(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func markProductionReady(t *testing.T, db *sql.DB, designID, versionID string) string {
	t.Helper()
	ctx := context.Background()
	prodID := uuid.NewString()
	now := time.Now().UTC()
	if err := postgres.NewAssetRepo(db).Insert(ctx, domain.Asset{
		ID: prodID, Type: domain.AssetTypeProduction, OwnerRef: designID,
		StorageKey: "private/blobs/metaldogs/production/2026/11/" + prodID + ".png",
		MimeType:   "image/png", SizeBytes: 1, Width: 64, Height: 64,
		Checksum: "sha256:late", CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE metaldogs_design_versions
		SET production_status = 'ready', production_asset_id = $2, production_checksum = $3
		WHERE id = $1
	`, versionID, prodID, "sha256:late"); err != nil {
		t.Fatal(err)
	}
	return prodID
}
