package metaldogs_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/internal/application/integration"
	platformjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainorder "github.com/akarso/shopanda/internal/domain/order"
	infraPostgres "github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
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

func TestSnapshotOrderJobID_IsV5(t *testing.T) {
	t.Parallel()
	const orderID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	id := metaldogs.SnapshotOrderJobID(orderID)
	if id != metaldogs.SnapshotOrderJobID("  "+orderID+"  ") {
		t.Fatalf("trim mismatch %s", id)
	}
	if id != metaldogs.SnapshotOrderJobID(strings.ToUpper(orderID)) {
		t.Fatalf("case mismatch %s", id)
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 5 {
		t.Fatalf("id %s version %d err %v", id, parsed.Version(), err)
	}
	if id == metaldogs.SnapshotOrderJobID("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb") {
		t.Fatal("different orders share a job id")
	}
}

func TestSnapshotOrder_FlagOffRegistersNothing(t *testing.T) {
	t.Parallel()
	app := testApp(t, true)
	initMetalDogs(t, app)
	for _, sj := range app.SyncJobs() {
		if sj.Job.Name == metaldogs.SnapshotOrderJobName || sj.Job.Name == metaldogs.SnapshotOrderDispatchJobName {
			t.Fatalf("flag off registered %+v", sj)
		}
	}
}

func TestSnapshotOrder_FlagOnRegistersManualAndDispatcher(t *testing.T) {
	t.Parallel()
	app := testApp(t, true)
	app.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	p := initMetalDogs(t, app)
	if p.OrderSource() != nil {
		t.Fatal("flag on must not bind an order source")
	}
	var manual, event bool
	for _, sj := range app.SyncJobs() {
		switch sj.Job.Name {
		case metaldogs.SnapshotOrderJobName:
			manual = true
			if sj.JobType != "integration.sync.metaldogs.snapshot_order_design" {
				t.Fatalf("type %s", sj.JobType)
			}
			if sj.Job.Trigger.Kind != extapi.SyncTriggerManual {
				t.Fatalf("snapshot trigger %+v", sj.Job.Trigger)
			}
		case metaldogs.SnapshotOrderDispatchJobName:
			event = true
			if sj.JobType != "integration.sync.metaldogs.dispatch_snapshot_order" {
				t.Fatalf("type %s", sj.JobType)
			}
			if sj.Job.Trigger.Kind != extapi.SyncTriggerEvent || sj.Job.Trigger.EventName != extapi.EventOrderCreated {
				t.Fatalf("dispatch trigger %+v", sj.Job.Trigger)
			}
		}
	}
	if !manual || !event {
		t.Fatalf("manual=%v event=%v", manual, event)
	}
}

func TestSnapshotOrder_Job(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "snapshot_order")
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
	clock := testutil.NewFakeClock(time.Date(2026, 11, 22, 20, 0, 0, 0, time.UTC))
	p.SetSnapshotClock(clock)
	src := memory.NewOrderSource()
	p.SetOrderSource(src)
	handler := findSnapshotHandler(t, app)
	snaps := postgres.NewOrderDesignSnapshotRepo(db)
	designs := postgres.NewDesignRepo(db)
	cat := &application.TemplateCatalog{Store: postgres.NewTemplateStore(db), IDs: ports.UUIDGenerator{}}

	t.Run("one design line", func(t *testing.T) {
		seed := seedApprovedDesign(t, db, cat, "tee-one", "prompt-one", true)
		orderID := uuid.NewString()
		variantID := uuid.NewString()
		src.Add(orderID, clock.Now(), []ports.OrderLine{{
			VariantID: variantID, SKU: "TEE-BLK-M", Quantity: 1, DesignID: seed.Design.ID,
		}})
		log := &snapLog{}
		if err := runSnapshot(t, handler, orderID, eventPayload(orderID), log); err != nil {
			t.Fatal(err)
		}
		got, ok, err := snaps.Get(ctx, orderID, variantID)
		if err != nil || !ok {
			t.Fatalf("get ok=%v err=%v", ok, err)
		}
		if got.DesignID != seed.Design.ID || got.DesignVersionID != seed.Version.ID || got.SKU != "TEE-BLK-M" ||
			got.TemplateSlug != seed.Template.Slug || got.TemplateVersion != seed.Template.Version ||
			got.Texts["dog_name"] != "Burek" || got.Provider != "openai" || got.Model != "gpt-image" ||
			got.PromptVersion != "prompt-v7" || got.ArtworkAssetID != seed.ArtworkID ||
			got.ArtworkChecksum != seed.ArtworkChecksum || got.ProductionAssetID != seed.ProductionID ||
			got.ProductionChecksum != "sha256:production" || !got.CreatedAt.Equal(clock.Now()) {
			t.Fatalf("%+v", got)
		}
		status, updated := designStatus(t, designs, seed.Design.ID)
		if status != domain.DesignOrdered || !updated.Equal(clock.Now()) {
			t.Fatalf("status=%s updated=%s", status, updated)
		}
		if !log.has("metaldogs.snapshot_order_design.written", orderID, variantID, seed.Design.ID) {
			t.Fatalf("log %+v", log.lines)
		}
		if log.contains("secret@example.com") || log.contains("cust-1") {
			t.Fatalf("log leaked customer data %+v", log.lines)
		}
	})

	t.Run("two sizes", func(t *testing.T) {
		seed := seedApprovedDesign(t, db, cat, "tee-sizes", "prompt-sizes", false)
		orderID := uuid.NewString()
		variantM := uuid.NewString()
		variantL := uuid.NewString()
		before := designUpdated(t, designs, seed.Design.ID)
		src.Add(orderID, clock.Now(), []ports.OrderLine{
			{VariantID: variantM, SKU: "TEE-M", Quantity: 1, DesignID: seed.Design.ID},
			{VariantID: variantL, SKU: "TEE-L", Quantity: 2, DesignID: seed.Design.ID},
		})
		if err := runSnapshot(t, handler, orderID, map[string]interface{}{"order_id": orderID}, &snapLog{}); err != nil {
			t.Fatal(err)
		}
		listed, err := snaps.ListByOrder(ctx, orderID)
		if err != nil || len(listed) != 2 {
			t.Fatalf("rows %+v err=%v", listed, err)
		}
		status, updated := designStatus(t, designs, seed.Design.ID)
		if status != domain.DesignOrdered || !updated.After(before) {
			t.Fatalf("status=%s updated=%s before=%s", status, updated, before)
		}
	})

	t.Run("run twice", func(t *testing.T) {
		seed := seedApprovedDesign(t, db, cat, "tee-twice", "prompt-twice", false)
		orderID := uuid.NewString()
		variantID := uuid.NewString()
		src.Add(orderID, clock.Now(), []ports.OrderLine{{
			VariantID: variantID, SKU: "TEE-M", Quantity: 1, DesignID: seed.Design.ID,
		}})
		if err := runSnapshot(t, handler, orderID, map[string]interface{}{"order_id": orderID}, &snapLog{}); err != nil {
			t.Fatal(err)
		}
		_, updated := designStatus(t, designs, seed.Design.ID)
		clock.Advance(time.Minute)
		log := &snapLog{}
		if err := runSnapshot(t, handler, orderID, map[string]interface{}{"order_id": orderID}, log); err != nil {
			t.Fatal(err)
		}
		listed, err := snaps.ListByOrder(ctx, orderID)
		if err != nil || len(listed) != 1 || listed[0].SKU != "TEE-M" {
			t.Fatalf("rows %+v err=%v", listed, err)
		}
		_, again := designStatus(t, designs, seed.Design.ID)
		if !again.Equal(updated) {
			t.Fatalf("second run moved updated_at %s → %s", updated, again)
		}
		if !log.has("metaldogs.snapshot_order_design.duplicate", orderID, variantID, seed.Design.ID) {
			t.Fatalf("log %+v", log.lines)
		}
	})

	t.Run("line without design id", func(t *testing.T) {
		seed := seedApprovedDesign(t, db, cat, "tee-plain", "prompt-plain", false)
		orderID := uuid.NewString()
		variantDesign := uuid.NewString()
		variantPlain := uuid.NewString()
		src.Add(orderID, clock.Now(), []ports.OrderLine{
			{VariantID: variantPlain, SKU: "MUG", Quantity: 1},
			{VariantID: variantDesign, SKU: "TEE-M", Quantity: 1, DesignID: seed.Design.ID},
		})
		if err := runSnapshot(t, handler, orderID, map[string]interface{}{"order_id": orderID}, &snapLog{}); err != nil {
			t.Fatal(err)
		}
		listed, err := snaps.ListByOrder(ctx, orderID)
		if err != nil || len(listed) != 1 || listed[0].VariantID != variantDesign {
			t.Fatalf("rows %+v err=%v", listed, err)
		}
		if _, ok, err := snaps.Get(ctx, orderID, variantPlain); err != nil || ok {
			t.Fatalf("plain line snapshotted ok=%v err=%v", ok, err)
		}
	})

	t.Run("design deleted", func(t *testing.T) {
		kept := seedApprovedDesign(t, db, cat, "tee-kept", "prompt-kept", false)
		gone := seedApprovedDesign(t, db, cat, "tee-gone", "prompt-gone", false)
		deleteDesign(t, db, gone.Design.ID)
		orderID := uuid.NewString()
		src.Add(orderID, clock.Now(), []ports.OrderLine{
			{VariantID: uuid.NewString(), SKU: "TEE-M", Quantity: 1, DesignID: kept.Design.ID},
			{VariantID: uuid.NewString(), SKU: "TEE-L", Quantity: 1, DesignID: gone.Design.ID},
		})
		log := &snapLog{}
		err := runSnapshot(t, handler, orderID, map[string]interface{}{"order_id": orderID}, log)
		if err == nil {
			t.Fatal("expected failure")
		}
		listed, listErr := snaps.ListByOrder(ctx, orderID)
		if listErr != nil || len(listed) != 0 {
			t.Fatalf("rows %+v err=%v", listed, listErr)
		}
		status, _ := designStatus(t, designs, kept.Design.ID)
		if status != domain.DesignApproved {
			t.Fatalf("kept design status %s", status)
		}
		if !log.has("metaldogs.snapshot_order_design.failed", orderID, "", gone.Design.ID) {
			t.Fatalf("log %+v err=%v", log.lines, err)
		}
	})

	t.Run("no approved version", func(t *testing.T) {
		preview := seedPreviewDesign(t, db, cat, "tee-preview", "prompt-preview")
		orderID := uuid.NewString()
		variantID := uuid.NewString()
		src.Add(orderID, clock.Now(), []ports.OrderLine{{
			VariantID: variantID, SKU: "TEE-M", Quantity: 1, DesignID: preview.ID,
		}})
		if err := runSnapshot(t, handler, orderID, map[string]interface{}{"order_id": orderID}, &snapLog{}); err == nil {
			t.Fatal("expected failure")
		}
		listed, err := snaps.ListByOrder(ctx, orderID)
		if err != nil || len(listed) != 0 {
			t.Fatalf("rows %+v err=%v", listed, err)
		}
		status, _ := designStatus(t, designs, preview.ID)
		if status != domain.DesignPreviewReady {
			t.Fatalf("status %s", status)
		}
	})

	t.Run("template republished", func(t *testing.T) {
		seed := seedApprovedDesign(t, db, cat, "tee-republish", "prompt-v1", true)
		orderID := uuid.NewString()
		variantID := uuid.NewString()
		src.Add(orderID, clock.Now(), []ports.OrderLine{{
			VariantID: variantID, SKU: "TEE-M", Quantity: 1, DesignID: seed.Design.ID,
		}})
		if err := runSnapshot(t, handler, orderID, map[string]interface{}{"order_id": orderID}, &snapLog{}); err != nil {
			t.Fatal(err)
		}
		before, ok, err := snaps.Get(ctx, orderID, variantID)
		if err != nil || !ok {
			t.Fatal(err)
		}
		if _, err := loadTemplateFile(t, cat, "tee-republish", "prompt-v2"); err != nil {
			t.Fatal(err)
		}
		if _, err := cat.Publish(ctx, "tee-republish", 0); err != nil {
			t.Fatal(err)
		}
		clock.Advance(time.Minute)
		if err := runSnapshot(t, handler, orderID, map[string]interface{}{"order_id": orderID}, &snapLog{}); err != nil {
			t.Fatal(err)
		}
		after, ok, err := snaps.Get(ctx, orderID, variantID)
		if err != nil || !ok {
			t.Fatal(err)
		}
		if after.TemplateVersion != before.TemplateVersion || after.TemplateSlug != before.TemplateSlug ||
			after.ArtworkChecksum != before.ArtworkChecksum || after.ProductionChecksum != before.ProductionChecksum ||
			after.DesignVersionID != before.DesignVersionID || !after.CreatedAt.Equal(before.CreatedAt) {
			t.Fatalf("before %+v after %+v", before, after)
		}
	})

	t.Run("production not ready", func(t *testing.T) {
		seed := seedApprovedDesign(t, db, cat, "tee-pending", "prompt-pending", false)
		orderID := uuid.NewString()
		variantID := uuid.NewString()
		src.Add(orderID, clock.Now(), []ports.OrderLine{{
			VariantID: variantID, SKU: "TEE-M", Quantity: 1, DesignID: seed.Design.ID,
		}})
		if err := runSnapshot(t, handler, orderID, map[string]interface{}{"order_id": orderID}, &snapLog{}); err != nil {
			t.Fatal(err)
		}
		got, ok, err := snaps.Get(ctx, orderID, variantID)
		if err != nil || !ok {
			t.Fatal(err)
		}
		if got.ProductionAssetID != "" || got.ProductionChecksum != "" {
			t.Fatalf("production %+v", got)
		}
		if got.ArtworkChecksum == "" {
			t.Fatal("artwork checksum empty")
		}
	})

	t.Run("duplicate enqueue is one job", func(t *testing.T) {
		seed := seedApprovedDesign(t, db, cat, "tee-enqueue-dup", "prompt-enqueue-dup", true)
		orderID := uuid.NewString()
		variantID := uuid.NewString()
		src.Add(orderID, clock.Now(), []ports.OrderLine{{
			VariantID: variantID, SKU: "TEE-M", Quantity: 1, DesignID: seed.Design.ID,
		}})
		if err := p.EnqueueSnapshotOrder(ctx, orderID); err != nil {
			t.Fatal(err)
		}
		if err := p.EnqueueSnapshotOrder(ctx, strings.ToUpper(orderID)); err != nil {
			t.Fatal(err)
		}
		jobID := metaldogs.SnapshotOrderJobID(orderID)
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE id = $1`, jobID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("jobs=%d", n)
		}
		worker := platformjobs.NewWorker(queue, logger.NewWithWriter(io.Discard, "error"), time.Millisecond)
		if err := integration.RegisterSyncJobHandlers(app, worker); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE jobs SET run_at = NOW() WHERE id = $1 AND status = 'pending'`, jobID); err != nil {
			t.Fatal(err)
		}
		worker.ProcessOne(ctx)
		var jobStatus string
		if err := db.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = $1`, jobID).Scan(&jobStatus); err != nil {
			t.Fatal(err)
		}
		if jobStatus != "done" {
			t.Fatalf("status=%s", jobStatus)
		}
		var snapsN int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM metaldogs_order_design_snapshots WHERE order_id = $1`, orderID).Scan(&snapsN); err != nil {
			t.Fatal(err)
		}
		if snapsN != 1 {
			t.Fatalf("snapshots=%d", snapsN)
		}
		if got, _ := designStatus(t, designs, seed.Design.ID); got != domain.DesignOrdered {
			t.Fatalf("status=%s", got)
		}
	})

	t.Run("missing design retries then terminal failed", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `DELETE FROM jobs WHERE type = $1`, "integration.sync.metaldogs.snapshot_order_design"); err != nil {
			t.Fatal(err)
		}
		orderID := uuid.NewString()
		missing := uuid.NewString()
		src.Add(orderID, clock.Now(), []ports.OrderLine{{
			VariantID: uuid.NewString(), SKU: "TEE-M", Quantity: 1, DesignID: missing,
		}})
		if err := p.EnqueueSnapshotOrder(ctx, orderID); err != nil {
			t.Fatal(err)
		}
		worker := platformjobs.NewWorker(queue, logger.NewWithWriter(io.Discard, "error"), time.Millisecond)
		if err := integration.RegisterSyncJobHandlers(app, worker); err != nil {
			t.Fatal(err)
		}
		driveSnapshotUntilFailed(t, db, worker, metaldogs.SnapshotOrderJobID(orderID))
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM metaldogs_order_design_snapshots WHERE order_id = $1`, orderID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("snapshots=%d", n)
		}
	})

	t.Run("duplicate order.created is one snapshot job", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `DELETE FROM jobs WHERE type = $1 OR type = $2`,
			"integration.sync.metaldogs.snapshot_order_design",
			"integration.sync.metaldogs.dispatch_snapshot_order"); err != nil {
			t.Fatal(err)
		}
		seed := seedApprovedDesign(t, db, cat, "tee-dup-event", "prompt-dup-event", true)
		orderID := uuid.NewString()
		variantID := uuid.NewString()
		src.Add(orderID, clock.Now(), []ports.OrderLine{{
			VariantID: variantID, SKU: "TEE-M", Quantity: 1, DesignID: seed.Design.ID,
		}})
		log := logger.NewWithWriter(io.Discard, "error")
		if err := integration.RegisterSyncJobEventTriggers(app, app.Bus, queue, log); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err := app.Bus.Publish(ctx, event.Event{
				Name: extapi.EventOrderCreated,
				ID:   uuid.NewString(),
				Data: domainorder.OrderCreatedData{OrderID: orderID, ItemCount: 1},
			}); err != nil {
				t.Fatal(err)
			}
		}
		dispatchType := "integration.sync.metaldogs.dispatch_snapshot_order"
		deadline := time.Now().Add(2 * time.Second)
		var n int
		var sample string
		for {
			if err := db.QueryRowContext(ctx, `
				SELECT count(*) FROM jobs
				WHERE type = $1
				  AND payload->'payload'->>'order_id' = $2
			`, dispatchType, orderID).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n >= 2 || time.Now().After(deadline) {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if n != 2 {
			_ = db.QueryRowContext(ctx, `
				SELECT coalesce(payload::text, '') FROM jobs
				WHERE type = $1 ORDER BY created_at DESC LIMIT 1
			`, dispatchType).Scan(&sample)
			t.Fatalf("dispatcher jobs=%d payload=%s", n, sample)
		}
		worker := platformjobs.NewWorker(queue, log, time.Millisecond)
		if err := integration.RegisterSyncJobHandlers(app, worker); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4; i++ {
			if _, err := db.ExecContext(ctx, `
				UPDATE jobs SET run_at = NOW()
				WHERE type = $1 AND status = 'pending'
				  AND payload->'payload'->>'order_id' = $2
			`, dispatchType, orderID); err != nil {
				t.Fatal(err)
			}
			worker.ProcessOne(ctx)
		}
		jobID := metaldogs.SnapshotOrderJobID(orderID)
		var snapJobs int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE id = $1`, jobID).Scan(&snapJobs); err != nil {
			t.Fatal(err)
		}
		if snapJobs != 1 {
			t.Fatalf("snapshot jobs=%d", snapJobs)
		}
		if _, err := db.ExecContext(ctx, `UPDATE jobs SET run_at = NOW() WHERE id = $1 AND status = 'pending'`, jobID); err != nil {
			t.Fatal(err)
		}
		worker.ProcessOne(ctx)
		var snapsN int
		var jobStatus string
		if err := db.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = $1`, jobID).Scan(&jobStatus); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM metaldogs_order_design_snapshots WHERE order_id = $1
		`, orderID).Scan(&snapsN); err != nil {
			t.Fatal(err)
		}
		var dispatchNotDone int
		if err := db.QueryRowContext(ctx, `
			SELECT count(*) FILTER (WHERE status <> 'done')
			FROM jobs
			WHERE type = $1 AND payload->'payload'->>'order_id' = $2
		`, dispatchType, orderID).Scan(&dispatchNotDone); err != nil {
			t.Fatal(err)
		}
		if snapsN != 1 || jobStatus != "done" || dispatchNotDone != 0 {
			t.Fatalf("snapshots=%d snapshot_status=%s dispatch_not_done=%d", snapsN, jobStatus, dispatchNotDone)
		}
		if status, _ := designStatus(t, designs, seed.Design.ID); status != domain.DesignOrdered {
			t.Fatalf("status=%s", status)
		}
	})

	t.Run("unbound order source ends failed", func(t *testing.T) {
		p.SetOrderSource(nil)
		t.Cleanup(func() { p.SetOrderSource(src) })
		orderID := uuid.NewString()
		if err := p.EnqueueSnapshotOrder(ctx, orderID); err != nil {
			t.Fatal(err)
		}
		jobID := metaldogs.SnapshotOrderJobID(orderID)
		worker := platformjobs.NewWorker(queue, logger.NewWithWriter(io.Discard, "error"), time.Millisecond)
		if err := integration.RegisterSyncJobHandlers(app, worker); err != nil {
			t.Fatal(err)
		}
		driveSnapshotUntilFailed(t, db, worker, jobID)
		listed, err := queue.List(ctx, platformjobs.ListFilter{
			Type:   "integration.sync.metaldogs.snapshot_order_design",
			Status: platformjobs.StatusFailed,
			Limit:  50,
		})
		if err != nil {
			t.Fatal(err)
		}
		var seen bool
		for _, row := range listed {
			if row.ID == jobID {
				seen = true
			}
		}
		if !seen {
			t.Fatalf("jobs:list missed %s", jobID)
		}
		var lastError sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT last_error FROM jobs WHERE id = $1`, jobID).Scan(&lastError); err != nil {
			t.Fatal(err)
		}
		if !lastError.Valid || !strings.Contains(lastError.String, "order source is not bound") {
			t.Fatalf("last_error=%v", lastError)
		}
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM metaldogs_order_design_snapshots WHERE order_id = $1`, orderID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("snapshots=%d", n)
		}
	})

	t.Run("commit failure names the design that failed", func(t *testing.T) {
		first := seedApprovedDesign(t, db, cat, "tee-commit-a", "prompt-commit-a", false)
		second := seedApprovedDesign(t, db, cat, "tee-commit-b", "prompt-commit-b", false)
		if _, err := db.ExecContext(ctx, `UPDATE metaldogs_designs SET status = $2 WHERE id = $1`, second.Design.ID, domain.DesignExpired); err != nil {
			t.Fatal(err)
		}
		orderID := uuid.NewString()
		v1, v2 := uuid.NewString(), uuid.NewString()
		at := clock.Now()
		lineA := snapshotFromSeed(t, orderID, v1, "TEE-A", first, at)
		lineB := snapshotFromSeed(t, orderID, v2, "TEE-B", second, at)
		_, err := snaps.CommitOrder(ctx, []domain.OrderDesignSnapshot{lineA, lineB}, at)
		var de *ports.SnapshotDesignError
		if !errors.As(err, &de) || de.DesignID != second.Design.ID {
			t.Fatalf("%v", err)
		}
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM metaldogs_order_design_snapshots WHERE order_id = $1`, orderID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("snapshots=%d", n)
		}
		if status, _ := designStatus(t, designs, first.Design.ID); status != domain.DesignApproved {
			t.Fatalf("first status=%s", status)
		}
	})

	t.Run("soft-deleted artwork fails the commit", func(t *testing.T) {
		seed := seedApprovedDesign(t, db, cat, "tee-dead-art", "prompt-dead-art", false)
		at := clock.Now()
		if _, err := db.ExecContext(ctx, `UPDATE metaldogs_assets SET deleted_at = $2 WHERE id = $1`, seed.ArtworkID, at); err != nil {
			t.Fatal(err)
		}
		orderID := uuid.NewString()
		line := snapshotFromSeed(t, orderID, uuid.NewString(), "TEE-DEAD", seed, at)
		_, err := snaps.CommitOrder(ctx, []domain.OrderDesignSnapshot{line}, at)
		var de *ports.SnapshotDesignError
		if !errors.As(err, &de) || de.DesignID != seed.Design.ID || !strings.Contains(err.Error(), "missing or deleted") {
			t.Fatalf("%v", err)
		}
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM metaldogs_order_design_snapshots WHERE order_id = $1`, orderID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("snapshots=%d", n)
		}
		if status, _ := designStatus(t, designs, seed.Design.ID); status != domain.DesignApproved {
			t.Fatalf("status=%s", status)
		}
	})

	t.Run("soft-deleted production fails the commit", func(t *testing.T) {
		seed := seedApprovedDesign(t, db, cat, "tee-dead-prod", "prompt-dead-prod", true)
		at := clock.Now()
		if _, err := db.ExecContext(ctx, `UPDATE metaldogs_assets SET deleted_at = $2 WHERE id = $1`, seed.ProductionID, at); err != nil {
			t.Fatal(err)
		}
		orderID := uuid.NewString()
		line := snapshotFromSeed(t, orderID, uuid.NewString(), "TEE-DEAD-PROD", seed, at)
		line.ProductionAssetID = seed.ProductionID
		line.ProductionChecksum = "sha256:production"
		_, err := snaps.CommitOrder(ctx, []domain.OrderDesignSnapshot{line}, at)
		var de *ports.SnapshotDesignError
		if !errors.As(err, &de) || de.DesignID != seed.Design.ID || !strings.Contains(err.Error(), seed.ProductionID) {
			t.Fatalf("%v", err)
		}
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM metaldogs_order_design_snapshots WHERE order_id = $1`, orderID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("snapshots=%d", n)
		}
		if status, _ := designStatus(t, designs, seed.Design.ID); status != domain.DesignApproved {
			t.Fatalf("status=%s", status)
		}
		assertAssetStill := func(id string, active bool) {
			t.Helper()
			var deleted sql.NullTime
			if err := db.QueryRowContext(ctx, `SELECT deleted_at FROM metaldogs_assets WHERE id = $1`, id).Scan(&deleted); err != nil {
				t.Fatal(err)
			}
			if deleted.Valid == active {
				t.Fatalf("asset %s active=%v deleted=%v", id, active, deleted)
			}
		}
		assertAssetStill(seed.ArtworkID, true)
		assertAssetStill(seed.ProductionID, false)
	})
}

func snapshotFromSeed(t *testing.T, orderID, variantID, sku string, seed seeded, at time.Time) domain.OrderDesignSnapshot {
	t.Helper()
	snap, err := domain.NewOrderDesignSnapshot(
		orderID, variantID, seed.Design.ID, seed.Version.ID, sku,
		seed.Template.Slug, seed.Template.Version, seed.Version.Texts,
		seed.Version.Provider, seed.Version.Model, seed.Version.PromptVersion,
		seed.ArtworkID, seed.ArtworkChecksum,
		"", "",
		at,
	)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func eventPayload(orderID string) map[string]interface{} {
	return map[string]interface{}{
		"data": map[string]interface{}{
			"order_id":      orderID,
			"customer_id":   "cust-1",
			"contact_email": "secret@example.com",
			"currency":      "PLN",
			"item_count":    1,
		},
	}
}

func runSnapshot(t *testing.T, handler extapi.SyncJobHandler, orderID string, payload map[string]interface{}, log extapi.SyncLogger) error {
	t.Helper()
	return handler(context.Background(), extapi.SyncJobContext{
		JobID:   metaldogs.SnapshotOrderJobID(orderID),
		Name:    metaldogs.SnapshotOrderJobName,
		Payload: payload,
		Logger:  log,
	})
}

func findSnapshotHandler(t *testing.T, app *plugin.App) extapi.SyncJobHandler {
	t.Helper()
	for _, reg := range app.SyncJobs() {
		if reg.Job.Name == metaldogs.SnapshotOrderJobName {
			return reg.Job.Handler
		}
	}
	t.Fatal("snapshot handler missing")
	return nil
}

type seeded struct {
	Design          domain.Design
	Version         domain.DesignVersion
	Template        domain.Template
	ArtworkID       string
	ArtworkChecksum string
	ProductionID    string
}

func seedApprovedDesign(t *testing.T, db *sql.DB, cat *application.TemplateCatalog, slug, prompt string, productionReady bool) seeded {
	t.Helper()
	ctx := context.Background()
	if _, err := loadTemplateFile(t, cat, slug, prompt); err != nil {
		t.Fatal(err)
	}
	pub, err := cat.Publish(ctx, slug, 0)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 22, 18, 0, 0, 0, time.UTC)
	assets := postgres.NewAssetRepo(db)
	designs := postgres.NewDesignRepo(db)
	versions := postgres.NewDesignVersionRepo(db)
	sourceID := uuid.NewString()
	artID := uuid.NewString()
	designID := uuid.NewString()
	verID := uuid.NewString()
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
		ID: designID, TemplateID: pub.ID, TemplateVersion: pub.Version,
		Status: domain.DesignDraft, Texts: map[string]string{"dog_name": "Burek"},
		SourceAssetID: sourceID, OwnerTokenHash: sum[:],
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(domain.DesignRetentionUnordered),
	}
	if err := designs.CreateBound(ctx, d, now.Add(domain.SourceAttachedRetention)); err != nil {
		t.Fatal(err)
	}
	artSum := "sha256:artwork-" + artID
	if err := assets.Insert(ctx, domain.Asset{
		ID: artID, Type: domain.AssetTypeArtwork, OwnerRef: designID,
		StorageKey: "private/blobs/metaldogs/artwork/2026/11/" + artID + ".png",
		MimeType:   "image/png", SizeBytes: 1, Width: 64, Height: 64,
		Checksum: artSum, CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE metaldogs_designs SET status = $1, updated_at = $2 WHERE id = $3`, domain.DesignPreviewReady, now, designID); err != nil {
		t.Fatal(err)
	}
	d.Status = domain.DesignPreviewReady
	d.UpdatedAt = now
	if err := d.Approve(verID, now); err != nil {
		t.Fatal(err)
	}
	v, err := domain.NewDesignVersion(
		verID, designID, pub.ID, pub.Version, d.Texts,
		artID, artSum, "openai", "gpt-image", "prompt-v7", []byte(`{}`), now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := versions.ApproveBound(ctx, d, v); err != nil {
		t.Fatal(err)
	}
	out := seeded{Design: d, Version: v, Template: pub, ArtworkID: artID, ArtworkChecksum: artSum}
	out.Design.Status = domain.DesignApproved
	out.Design.ApprovedVersionID = verID
	if productionReady {
		prodID := uuid.NewString()
		if err := assets.Insert(ctx, domain.Asset{
			ID: prodID, Type: domain.AssetTypeProduction, OwnerRef: designID,
			StorageKey: "private/blobs/metaldogs/production/2026/11/" + prodID + ".png",
			MimeType:   "image/png", SizeBytes: 1, Width: 64, Height: 64,
			Checksum: "sha256:production", CreatedAt: now, DeleteAfter: now.Add(2 * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE metaldogs_design_versions
			SET production_status = 'ready', production_asset_id = $2, production_checksum = $3
			WHERE id = $1
		`, verID, prodID, "sha256:production"); err != nil {
			t.Fatal(err)
		}
		out.ProductionID = prodID
	}
	return out
}

func seedPreviewDesign(t *testing.T, db *sql.DB, cat *application.TemplateCatalog, slug, prompt string) domain.Design {
	t.Helper()
	seed := seedApprovedDesign(t, db, cat, slug, prompt, false)
	if _, err := db.ExecContext(context.Background(), `
		UPDATE metaldogs_designs SET approved_version_id = NULL, status = $2 WHERE id = $1
	`, seed.Design.ID, domain.DesignPreviewReady); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `DELETE FROM metaldogs_design_versions WHERE design_id = $1`, seed.Design.ID); err != nil {
		t.Fatal(err)
	}
	seed.Design.Status = domain.DesignPreviewReady
	seed.Design.ApprovedVersionID = ""
	return seed.Design
}

func deleteDesign(t *testing.T, db *sql.DB, designID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `UPDATE metaldogs_designs SET approved_version_id = NULL WHERE id = $1`, designID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM metaldogs_design_versions WHERE design_id = $1`, designID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM metaldogs_designs WHERE id = $1`, designID); err != nil {
		t.Fatal(err)
	}
}

func loadTemplateFile(t *testing.T, cat *application.TemplateCatalog, slug, prompt string) (domain.Template, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, slug+".yaml")
	if err := os.WriteFile(path, []byte(snapshotTemplateYAML(slug, prompt)), 0o640); err != nil {
		return domain.Template{}, err
	}
	loaded, err := cat.LoadDraft(context.Background(), path)
	if err != nil {
		return domain.Template{}, err
	}
	return loaded.Template, nil
}

func snapshotTemplateYAML(slug, prompt string) string {
	return fmt.Sprintf(`slug: %s
genre: black_metal
style_category: head_portrait
prompt_instructions:
  text: %s
  prompt_version: g2-test
model_parameters:
  model: gpt-image-2.5-sunburst
  width: 1024
  height: 1024
  seed: 1
photo_requirements:
  pose: head_front
text_fields:
  editable:
    - key: dog_name
      max_length: 12
      allowed_charset: latin_polish
  fixed: []
output_config:
  canvas_width: 1024
  canvas_height: 1024
  monochrome: true
  print_area: { x: 0, y: 0, width: 1024, height: 1024 }
`, slug, prompt)
}

func designStatus(t *testing.T, designs *postgres.DesignRepo, id string) (string, time.Time) {
	t.Helper()
	d, ok, err := designs.GetByID(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("design %s ok=%v err=%v", id, ok, err)
	}
	return d.Status, d.UpdatedAt
}

func designUpdated(t *testing.T, designs *postgres.DesignRepo, id string) time.Time {
	t.Helper()
	_, updated := designStatus(t, designs, id)
	return updated
}

func driveSnapshotUntilFailed(t *testing.T, db *sql.DB, worker *platformjobs.Worker, jobID string) {
	t.Helper()
	ctx := context.Background()
	var status string
	var attempts int
	for i := 0; i < 6; i++ {
		if _, err := db.ExecContext(ctx, `UPDATE jobs SET run_at = NOW() WHERE id = $1 AND status = 'pending'`, jobID); err != nil {
			t.Fatal(err)
		}
		worker.ProcessOne(ctx)
		if err := db.QueryRowContext(ctx, `SELECT status, attempts FROM jobs WHERE id = $1`, jobID).Scan(&status, &attempts); err != nil {
			t.Fatal(err)
		}
		if status == "failed" {
			if attempts < 2 {
				t.Fatalf("terminal without retry attempts=%d", attempts)
			}
			return
		}
	}
	t.Fatalf("status=%s attempts=%d", status, attempts)
}

type snapLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *snapLog) Info(msg string, fields map[string]interface{}) {
	l.add(msg, fields)
}

func (l *snapLog) Error(msg string, _ error, fields map[string]interface{}) {
	l.add(msg, fields)
}

func (l *snapLog) add(msg string, fields map[string]interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf("%s %v", msg, fields))
}

func (l *snapLog) has(msg, orderID, variantID, designID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line, msg) && strings.Contains(line, orderID) && strings.Contains(line, designID) && (variantID == "" || strings.Contains(line, variantID)) {
			return true
		}
	}
	return false
}

func (l *snapLog) contains(s string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line, s) {
			return true
		}
	}
	return false
}
