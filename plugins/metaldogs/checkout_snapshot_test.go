package metaldogs_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/integration"
	platformjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainorder "github.com/akarso/shopanda/internal/domain/order"
	infraPostgres "github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestCheckout_OrderSnapshot(t *testing.T) {
	db := openCheckoutDB(t)
	queue, err := infraPostgres.NewJobQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	offSrv, offApp := servePaidCheckout(t, db, false, queue)
	onSrv, onApp := servePaidCheckout(t, db, true, queue)
	worker := platformjobs.NewWorker(queue, onApp.Logger, time.Millisecond)
	if err := integration.RegisterSyncJobHandlers(onApp, worker); err != nil {
		t.Fatal(err)
	}
	dispatchType, err := extapi.SyncJobType(metaldogs.PluginName, metaldogs.SnapshotOrderDispatchJobName)
	if err != nil {
		t.Fatal(err)
	}
	snapshotType, err := extapi.SyncJobType(metaldogs.PluginName, metaldogs.SnapshotOrderJobName)
	if err != nil {
		t.Fatal(err)
	}
	cat := &application.TemplateCatalog{Store: postgres.NewTemplateStore(db), IDs: ports.UUIDGenerator{}}
	snaps := postgres.NewOrderDesignSnapshotRepo(db)
	designs := postgres.NewDesignRepo(db)
	variantM := teeVariantID(t, onSrv, "MDOGS-TEE-M")
	variantL := teeVariantID(t, onSrv, "MDOGS-TEE-L")

	t.Run("flag off enqueues nothing", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 1)
		cartID := addDesignLine(t, offSrv, design.ID, token, variantM)
		status, body := postCheckout(t, offSrv, cartID)
		if status != http.StatusCreated {
			t.Fatalf("status=%d body=%s", status, body)
		}
		orderID, _ := checkoutOrder(t, body)
		if n := waitOrderJobsQuiet(t, db, dispatchType, orderID, 0); n != 0 {
			t.Fatalf("dispatcher jobs=%d", n)
		}
		if n := countOrderJobs(t, db, snapshotType, orderID); n != 0 {
			t.Fatalf("snapshot jobs=%d", n)
		}
		if n := countSnapshots(t, snaps, orderID); n != 0 {
			t.Fatalf("snapshots=%d", n)
		}
		if got, _ := designStatus(t, designs, design.ID); got != domain.DesignApproved {
			t.Fatalf("status=%s", got)
		}
		for _, sj := range offApp.SyncJobs() {
			if sj.Job.Name == metaldogs.SnapshotOrderJobName || sj.Job.Name == metaldogs.SnapshotOrderDispatchJobName {
				t.Fatalf("flag off registered %+v", sj.Job.Name)
			}
		}
	})

	t.Run("custom tee", func(t *testing.T) {
		seed := seedApprovedDesign(t, db, cat, "tee-e2e-one", "prompt-e2e-one", true)
		token := setDesignOwnerToken(t, db, seed.Design.ID)
		cartID := addDesignLine(t, onSrv, seed.Design.ID, token, variantM)
		status, body := postCheckout(t, onSrv, cartID)
		if status != http.StatusCreated {
			t.Fatalf("status=%d body=%s", status, body)
		}
		assertOrderCarriesDesign(t, db, body, seed.Design.ID, variantM)
		orderID, _ := checkoutOrder(t, body)
		if n := waitOrderJobs(t, db, dispatchType, orderID, 1); n != 1 {
			t.Fatalf("dispatcher jobs=%d", n)
		}
		if got := jobIDByOrder(t, db, dispatchType, orderID); got != metaldogs.DispatchSnapshotOrderJobID(orderID) {
			t.Fatalf("dispatch id %s", got)
		}
		deliverOrderCreatedAgain(t, onApp, orderID)
		if n := waitOrderJobsQuiet(t, db, dispatchType, orderID, 1); n != 1 {
			t.Fatalf("dispatcher jobs after second delivery=%d", n)
		}
		driveJobsDone(t, db, worker, metaldogs.DispatchSnapshotOrderJobID(orderID), metaldogs.SnapshotOrderJobID(orderID))
		if n := countOrderJobs(t, db, snapshotType, orderID); n != 1 {
			t.Fatalf("snapshot jobs=%d", n)
		}
		if n := countOrderJobs(t, db, dispatchType, orderID); n != 1 {
			t.Fatalf("dispatcher jobs=%d", n)
		}
		got, ok, err := snaps.Get(context.Background(), orderID, variantM)
		if err != nil || !ok {
			t.Fatalf("get ok=%v err=%v", ok, err)
		}
		assertSnapshotCopied(t, got, orderID, variantM, "MDOGS-TEE-M", seed)
		statusName, updated := designStatus(t, designs, seed.Design.ID)
		if statusName != domain.DesignOrdered || !got.CreatedAt.Equal(updated) {
			t.Fatalf("status=%s updated=%s created=%s", statusName, updated, got.CreatedAt)
		}
	})

	t.Run("same design two sizes", func(t *testing.T) {
		seed := seedApprovedDesign(t, db, cat, "tee-e2e-sizes", "prompt-e2e-sizes", true)
		token := setDesignOwnerToken(t, db, seed.Design.ID)
		cartID := addDesignLine(t, onSrv, seed.Design.ID, token, variantM)
		status, body, _ := addDesignToCart(t, onSrv, seed.Design.ID, token, variantL, &http.Cookie{Name: sessionCartCookie, Value: cartID})
		if status != http.StatusOK {
			t.Fatalf("add L: status=%d body=%s", status, body)
		}
		status, body = postCheckout(t, onSrv, cartID)
		if status != http.StatusCreated {
			t.Fatalf("status=%d body=%s", status, body)
		}
		orderID, _ := checkoutOrder(t, body)
		if n := waitOrderJobs(t, db, dispatchType, orderID, 1); n != 1 {
			t.Fatalf("dispatcher jobs=%d", n)
		}
		driveJobsDone(t, db, worker, metaldogs.DispatchSnapshotOrderJobID(orderID), metaldogs.SnapshotOrderJobID(orderID))
		listed, err := snaps.ListByOrder(context.Background(), orderID)
		if err != nil {
			t.Fatal(err)
		}
		if len(listed) != 2 {
			t.Fatalf("snapshots=%d %+v", len(listed), listed)
		}
		byVariant := map[string]domain.OrderDesignSnapshot{}
		for _, row := range listed {
			byVariant[row.VariantID] = row
		}
		assertSnapshotCopied(t, byVariant[variantM], orderID, variantM, "MDOGS-TEE-M", seed)
		assertSnapshotCopied(t, byVariant[variantL], orderID, variantL, "MDOGS-TEE-L", seed)
		if got, _ := designStatus(t, designs, seed.Design.ID); got != domain.DesignOrdered {
			t.Fatalf("status=%s", got)
		}
	})

	t.Run("other product", func(t *testing.T) {
		other := insertPublishedOtherVariant(t, db)
		cartID := createPLNCart(t, onSrv)
		status, body := cartDo(t, onSrv, http.MethodPost, "/api/v1/carts/"+cartID+"/items", map[string]any{
			"variant_id": other,
			"quantity":   1,
		})
		if status != http.StatusOK {
			t.Fatalf("add: status=%d body=%s", status, body)
		}
		status, body = postCheckout(t, onSrv, cartID)
		if status != http.StatusCreated {
			t.Fatalf("status=%d body=%s", status, body)
		}
		orderID, _ := checkoutOrder(t, body)
		if n := waitOrderJobs(t, db, dispatchType, orderID, 1); n != 1 {
			t.Fatalf("dispatcher jobs=%d", n)
		}
		driveJobsDone(t, db, worker, metaldogs.DispatchSnapshotOrderJobID(orderID), metaldogs.SnapshotOrderJobID(orderID))
		if n := countSnapshots(t, snaps, orderID); n != 0 {
			t.Fatalf("snapshots=%d", n)
		}
	})
}

func servePaidCheckout(t *testing.T, db *sql.DB, snapshot bool, queue platformjobs.Queue) (*httptest.Server, *plugin.App) {
	t.Helper()
	app := testApp(t, true)
	app.Bootstrap = &plugin.Bootstrap{DB: db}
	if snapshot {
		app.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	}
	orders, err := infraPostgres.NewOrderRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.BindOrdersPort(pluginsdk.NewOrdersPort(orders)); err != nil {
		t.Fatal(err)
	}
	extReg := extensionapp.NewRegistry()
	app.SetExtensionRegistry(extReg)
	reg := plugin.NewRegistry(app.Logger)
	plugged := metaldogs.New()
	reg.Register(pluginsdk.Adapt(plugged))
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll %+v", summary)
	}
	if snapshot && plugged.OrderSource() == nil {
		t.Fatal("flag on left the order source unset")
	}
	if !snapshot && plugged.OrderSource() != nil {
		t.Fatal("flag off bound an order source")
	}
	extValues, err := infraPostgres.NewExtensionValueRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	port, ok := app.OrdersPort()
	if !ok {
		t.Fatal("orders port missing")
	}
	pluginsdk.AttachOrderLineExtensions(port, extensionapp.NewValueService(extReg, extValues))
	if queue != nil {
		if err := bindRependQueue(t, app, queue); err != nil {
			t.Fatal(err)
		}
		if err := integration.RegisterSyncJobEventTriggers(app, app.Bus, queue, app.Logger); err != nil {
			t.Fatal(err)
		}
	}
	return openStorefront(t, db, extReg, app, true), app
}

func setDesignOwnerToken(t *testing.T, db *sql.DB, designID string) string {
	t.Helper()
	token, hash, err := application.MintOwnerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE metaldogs_designs SET owner_token_hash = $2 WHERE id = $1`, designID, hash); err != nil {
		t.Fatal(err)
	}
	return token
}

func deliverOrderCreatedAgain(t *testing.T, app *plugin.App, orderID string) {
	t.Helper()
	if err := app.Bus.Publish(context.Background(), event.Event{
		Name: extapi.EventOrderCreated,
		ID:   uuid.NewString(),
		Data: domainorder.OrderCreatedData{OrderID: orderID, ItemCount: 1},
	}); err != nil {
		t.Fatal(err)
	}
}

func assertSnapshotCopied(t *testing.T, got domain.OrderDesignSnapshot, orderID, variantID, sku string, seed seeded) {
	t.Helper()
	if got.OrderID != orderID || got.VariantID != variantID || got.DesignID != seed.Design.ID ||
		got.DesignVersionID != seed.Version.ID || got.SKU != sku ||
		got.TemplateSlug != seed.Template.Slug || got.TemplateVersion != seed.Template.Version ||
		len(got.Texts) != 1 || got.Texts["dog_name"] != "Burek" ||
		got.Provider != "openai" || got.Model != "gpt-image" || got.PromptVersion != "prompt-v7" ||
		got.ArtworkAssetID != seed.ArtworkID || got.ArtworkChecksum != seed.ArtworkChecksum ||
		got.ProductionAssetID != seed.ProductionID || got.ProductionChecksum != "sha256:production" ||
		got.CreatedAt.IsZero() {
		t.Fatalf("%+v", got)
	}
}

func countSnapshots(t *testing.T, snaps *postgres.OrderDesignSnapshotRepo, orderID string) int {
	t.Helper()
	listed, err := snaps.ListByOrder(context.Background(), orderID)
	if err != nil {
		t.Fatal(err)
	}
	return len(listed)
}

func countOrderJobs(t *testing.T, db *sql.DB, jobType, orderID string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM jobs
		WHERE type = $1 AND payload->'payload'->>'order_id' = $2
	`, jobType, orderID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func jobIDByOrder(t *testing.T, db *sql.DB, jobType, orderID string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(context.Background(), `
		SELECT id FROM jobs
		WHERE type = $1 AND payload->'payload'->>'order_id' = $2
	`, jobType, orderID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func waitOrderJobs(t *testing.T, db *sql.DB, jobType, orderID string, want int) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		n := countOrderJobs(t, db, jobType, orderID)
		if n >= want || time.Now().After(deadline) {
			return n
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitOrderJobsQuiet(t *testing.T, db *sql.DB, jobType, orderID string, want int) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n := countOrderJobs(t, db, jobType, orderID)
		if n > want {
			return n
		}
		time.Sleep(5 * time.Millisecond)
	}
	return countOrderJobs(t, db, jobType, orderID)
}

func driveJobsDone(t *testing.T, db *sql.DB, worker *platformjobs.Worker, ids ...string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pending := false
		for _, id := range ids {
			var status string
			var last sql.NullString
			err := db.QueryRowContext(ctx, `SELECT status, last_error FROM jobs WHERE id = $1`, id).Scan(&status, &last)
			if err == sql.ErrNoRows {
				pending = true
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			switch status {
			case "done":
			case "failed":
				t.Fatalf("job %s failed: %s", id, last.String)
			default:
				pending = true
			}
		}
		if !pending {
			return
		}
		for _, id := range ids {
			if _, err := db.ExecContext(ctx, `UPDATE jobs SET run_at = NOW() WHERE id = $1 AND status = 'pending'`, id); err != nil {
				t.Fatal(err)
			}
		}
		if !worker.ProcessOne(ctx) {
			time.Sleep(5 * time.Millisecond)
		}
	}
	t.Fatalf("jobs not done: %v", ids)
}
