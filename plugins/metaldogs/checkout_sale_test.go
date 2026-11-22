package metaldogs_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestCheckout_DesignSale(t *testing.T) {
	db := openCheckoutDB(t)
	enabled, enabledApp, _, saleLog := serveCheckout(t, db, true)
	disabled, disabledApp, _, _ := serveCheckout(t, db, false)
	if disabledApp != nil {
		t.Fatal("disabled checkout server registered the plugin")
	}
	regs := enabledApp.CheckoutStepRegistrations()
	if len(regs) != 1 || regs[0].Position != "before:reserve_inventory" {
		t.Fatalf("registrations=%+v", regs)
	}
	named, ok := regs[0].Step.(interface{ Name() string })
	if !ok || named.Name() != "metaldogs_design_sale" {
		t.Fatalf("step=%T %#v", regs[0].Step, regs[0].Step)
	}

	variantM := teeVariantID(t, enabled, "MDOGS-TEE-M")
	variantL := teeVariantID(t, enabled, "MDOGS-TEE-L")

	t.Run("approved", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 1)
		cartID := addDesignLine(t, enabled, design.ID, token, variantM)
		status, body := postCheckout(t, enabled, cartID)
		if status != http.StatusCreated {
			t.Fatalf("status=%d body=%s", status, body)
		}
		assertOrderCarriesDesign(t, body, design.ID, variantM)
	})

	t.Run("same design two sizes", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 2)
		cartID := addDesignLine(t, enabled, design.ID, token, variantM)
		status, body, _ := addDesignToCart(t, enabled, design.ID, token, variantL, &http.Cookie{Name: sessionCartCookie, Value: cartID})
		if status != http.StatusOK {
			t.Fatalf("add L: status=%d body=%s", status, body)
		}
		status, body = postCheckout(t, enabled, cartID)
		if status != http.StatusCreated {
			t.Fatalf("status=%d body=%s", status, body)
		}
		assertOrderCarriesDesign(t, body, design.ID, variantM, variantL)
	})

	t.Run("ordered", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 3)
		cartID := addDesignLine(t, enabled, design.ID, token, variantM)
		if _, err := db.ExecContext(context.Background(), `UPDATE metaldogs_designs SET status = 'ordered' WHERE id = $1`, design.ID); err != nil {
			t.Fatal(err)
		}
		status, body := postCheckout(t, enabled, cartID)
		if status != http.StatusCreated {
			t.Fatalf("status=%d body=%s", status, body)
		}
		assertOrderCarriesDesign(t, body, design.ID, variantM)
	})

	t.Run("expired", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 4)
		cartID := addDesignLine(t, enabled, design.ID, token, variantM)
		if _, err := db.ExecContext(context.Background(), `UPDATE metaldogs_designs SET expires_at = now() - interval '1 hour' WHERE id = $1`, design.ID); err != nil {
			t.Fatal(err)
		}
		before := takeSaleSnap(t, db, variantM)
		status, body := postCheckout(t, enabled, cartID)
		assertDesignRefused(t, status, body, refusedLine{VariantID: variantM, Code: "design_expired"})
		assertSaleUnchanged(t, db, before, variantM)
		assertRefusalLogged(t, saleLog, cartID, []string{variantM}, []string{"design_expired"}, design.ID)
		status, body = postCheckout(t, disabled, cartID)
		if status != http.StatusCreated {
			t.Fatalf("plugin disabled status=%d body=%s", status, body)
		}
	})

	t.Run("deleted", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 5)
		cartID := addDesignLine(t, enabled, design.ID, token, variantM)
		if _, err := db.ExecContext(context.Background(), `DELETE FROM metaldogs_designs WHERE id = $1`, design.ID); err != nil {
			t.Fatal(err)
		}
		before := takeSaleSnap(t, db, variantM)
		status, body := postCheckout(t, enabled, cartID)
		assertDesignRefused(t, status, body, refusedLine{VariantID: variantM, Code: "design_not_found"})
		assertSaleUnchanged(t, db, before, variantM)
	})

	t.Run("draft", func(t *testing.T) {
		refuseStatus(t, db, enabled, variantM, "draft", "design_not_approved", 6)
	})

	t.Run("preview_ready", func(t *testing.T) {
		refuseStatus(t, db, enabled, variantM, "preview_ready", "design_not_approved", 7)
	})

	t.Run("missing design id", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 8)
		cartID := addDesignLine(t, enabled, design.ID, token, variantM)
		target := cartID + ":" + variantM
		if _, err := db.ExecContext(context.Background(), `DELETE FROM extension_values WHERE target_type = 'cart_item' AND target_id = $1 AND field_code = $2`, target, ports.DesignIDFieldCode); err != nil {
			t.Fatal(err)
		}
		before := takeSaleSnap(t, db, variantM)
		status, body := postCheckout(t, enabled, cartID)
		assertDesignRefused(t, status, body, refusedLine{VariantID: variantM, Code: "design_required"})
		assertSaleUnchanged(t, db, before, variantM)
	})

	t.Run("two bad lines", func(t *testing.T) {
		left, leftToken := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 9)
		right, rightToken := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 10)
		cartID := addDesignLine(t, enabled, left.ID, leftToken, variantM)
		status, body, _ := addDesignToCart(t, enabled, right.ID, rightToken, variantL, &http.Cookie{Name: sessionCartCookie, Value: cartID})
		if status != http.StatusOK {
			t.Fatalf("add L: status=%d body=%s", status, body)
		}
		if _, err := db.ExecContext(context.Background(), `UPDATE metaldogs_designs SET expires_at = now() - interval '1 hour' WHERE id = $1`, left.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(context.Background(), `UPDATE metaldogs_designs SET status = 'draft' WHERE id = $1`, right.ID); err != nil {
			t.Fatal(err)
		}
		before := takeSaleSnap(t, db, variantM, variantL)
		status, body = postCheckout(t, enabled, cartID)
		assertDesignRefused(t, status, body,
			refusedLine{VariantID: variantM, Code: "design_expired"},
			refusedLine{VariantID: variantL, Code: "design_not_approved"},
		)
		assertSaleUnchanged(t, db, before, variantM, variantL)
		assertRefusalLogged(t, saleLog, cartID, []string{variantM, variantL}, []string{"design_expired", "design_not_approved"}, left.ID, right.ID)
	})

	t.Run("logged-in customer", func(t *testing.T) {
		const customerID = "6f1b9c2e-7a34-4d1e-9c80-0a1b2c3d4e5f"
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 14)
		cartID := addDesignLine(t, enabled, design.ID, token, variantM)
		if _, err := db.ExecContext(context.Background(), `UPDATE carts SET customer_id = $2 WHERE id = $1`, cartID, customerID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(context.Background(), `UPDATE metaldogs_designs SET expires_at = now() - interval '1 hour' WHERE id = $1`, design.ID); err != nil {
			t.Fatal(err)
		}
		before := takeSaleSnap(t, db, variantM)
		status, body := postCheckoutAs(t, enabled, cartID, customerID)
		assertDesignRefused(t, status, body, refusedLine{VariantID: variantM, Code: "design_expired"})
		assertSaleUnchanged(t, db, before, variantM)
		assertRefusalLogged(t, saleLog, cartID, []string{variantM}, []string{"design_expired"}, design.ID, customerID)
	})

	t.Run("other product", func(t *testing.T) {
		other := insertPublishedOtherVariant(t, db)
		cartID := createPLNCart(t, enabled)
		status, body := cartDo(t, enabled, http.MethodPost, "/api/v1/carts/"+cartID+"/items", map[string]any{
			"variant_id": other,
			"quantity":   1,
		})
		if status != http.StatusOK {
			t.Fatalf("add: status=%d body=%s", status, body)
		}
		if _, err := db.ExecContext(context.Background(), `ALTER TABLE metaldogs_designs RENAME TO metaldogs_designs_hidden`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), `ALTER TABLE IF EXISTS metaldogs_designs_hidden RENAME TO metaldogs_designs`)
		})
		status, body = postCheckout(t, enabled, cartID)
		if status != http.StatusCreated {
			t.Fatalf("status=%d body=%s", status, body)
		}
		if got := orderVariantIDs(t, body); len(got) != 1 || got[0] != other {
			t.Fatalf("variants=%v", got)
		}
	})

	t.Run("cart port failure", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 11)
		cartID := addDesignLine(t, enabled, design.ID, token, variantM)
		faulty, _, plugged, _ := serveCheckout(t, db, true)
		plugged.FailCartGetForTest(errors.New("cart get failed"))
		before := takeSaleSnap(t, db, variantM)
		status, body := postCheckout(t, faulty, cartID)
		assertRetryableRefusal(t, status, body, "cart get failed")
		assertSaleUnchanged(t, db, before, variantM)
	})

	t.Run("catalog failure", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 12)
		cartID := addDesignLine(t, enabled, design.ID, token, variantM)
		faulty, _, plugged, _ := serveCheckout(t, db, true)
		plugged.FailCatalogFindForTest(errors.New("catalog down"))
		before := takeSaleSnap(t, db, variantM)
		status, body := postCheckout(t, faulty, cartID)
		assertRetryableRefusal(t, status, body, "catalog down")
		assertSaleUnchanged(t, db, before, variantM)
	})

	t.Run("design store failure", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 13)
		cartID := addDesignLine(t, enabled, design.ID, token, variantM)
		faulty, _, plugged, _ := serveCheckout(t, db, true)
		svc := plugged.DesignService()
		if svc == nil || svc.Designs == nil {
			t.Fatal("design store not wired")
		}
		svc.Designs = designGetErrorRepo{DesignRepository: svc.Designs}
		before := takeSaleSnap(t, db, variantM)
		status, body := postCheckout(t, faulty, cartID)
		assertRetryableRefusal(t, status, body, "design store down")
		assertSaleUnchanged(t, db, before, variantM)
	})
}

type designGetErrorRepo struct {
	ports.DesignRepository
}

func (designGetErrorRepo) GetByID(context.Context, string) (domain.Design, bool, error) {
	return domain.Design{}, false, errors.New("design store down")
}

func refuseStatus(t *testing.T, db *sql.DB, srv *httptest.Server, variantID, status, code string, version int) {
	t.Helper()
	design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), version)
	cartID := addDesignLine(t, srv, design.ID, token, variantID)
	if _, err := db.ExecContext(context.Background(), `UPDATE metaldogs_designs SET status = $2 WHERE id = $1`, design.ID, status); err != nil {
		t.Fatal(err)
	}
	before := takeSaleSnap(t, db, variantID)
	got, body := postCheckout(t, srv, cartID)
	assertDesignRefused(t, got, body, refusedLine{VariantID: variantID, Code: code})
	assertSaleUnchanged(t, db, before, variantID)
}

func openCheckoutDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testutil.OpenIsolatedWithCore(t, "checkout_sale")
	runCatalogSeed(t, db, repoCatalogDir(t))
	publishCatalogProduct(t, db)
	setTeeStock(t, db, 5)
	return db
}

func serveCheckout(t *testing.T, db *sql.DB, enable bool) (*httptest.Server, *plugin.App, *metaldogs.Plugin, *bytes.Buffer) {
	t.Helper()
	app := testApp(t, enable)
	logBuf := &bytes.Buffer{}
	app.Logger = logger.NewWithWriter(logBuf, "info")
	app.Bootstrap = &plugin.Bootstrap{DB: db}
	extReg := extensionapp.NewRegistry()
	app.SetExtensionRegistry(extReg)
	reg := plugin.NewRegistry(app.Logger)
	plugged := metaldogs.New()
	reg.Register(pluginsdk.Adapt(plugged))
	summary := reg.InitAll(app)
	if !enable {
		if summary.Initialized != 0 || summary.Failed != 1 {
			t.Fatalf("disabled InitAll summary = %+v", summary)
		}
		entries := reg.Entries()
		if len(entries) != 1 || entries[0].Err == nil || !strings.Contains(entries[0].Err.Error(), "disabled") {
			t.Fatalf("disabled init entries = %+v", entries)
		}
		if regs := app.CheckoutStepRegistrations(); len(regs) != 0 {
			t.Fatalf("disabled checkout steps = %+v", regs)
		}
		return openStorefront(t, db, extReg, nil, true), nil, nil, logBuf
	}
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll summary = %+v", summary)
	}
	return openStorefront(t, db, extReg, app, true), app, plugged, logBuf
}

func postCheckout(t *testing.T, srv *httptest.Server, cartID string) (int, []byte) {
	t.Helper()
	return postCheckoutAs(t, srv, cartID, "")
}

func postCheckoutAs(t *testing.T, srv *httptest.Server, cartID, customerID string) (int, []byte) {
	t.Helper()
	return cartDoAs(t, srv, http.MethodPost, "/api/v1/checkout", map[string]any{
		"cart_id":       cartID,
		"contact_email": "guest@example.com",
		"address": map[string]string{
			"first_name": "Ada",
			"last_name":  "Lovelace",
			"street":     "1 Logic Lane",
			"city":       "Warsaw",
			"postcode":   "00-001",
			"country":    "PL",
		},
	}, customerID)
}

type refusedLine struct {
	VariantID string `json:"variant_id"`
	Code      string `json:"code"`
}

func assertDesignRefused(t *testing.T, status int, body []byte, want ...refusedLine) {
	t.Helper()
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", status, body)
	}
	env := checkoutError(t, body)
	if env.Error.Code != "plugin.metaldogs.design_unavailable" || env.Error.Message != "Design no longer available" {
		t.Fatalf("code=%q message=%q body=%s", env.Error.Code, env.Error.Message, body)
	}
	if env.Error.Details.Retryable == nil || *env.Error.Details.Retryable {
		t.Fatalf("retryable=%v body=%s", env.Error.Details.Retryable, body)
	}
	if len(env.Error.Details.Lines) != len(want) {
		t.Fatalf("lines=%v want=%v body=%s", env.Error.Details.Lines, want, body)
	}
	for i, line := range want {
		if env.Error.Details.Lines[i] != line {
			t.Fatalf("line %d = %+v want %+v body=%s", i, env.Error.Details.Lines[i], line, body)
		}
	}
}

func assertRetryableRefusal(t *testing.T, status int, body []byte, secret string) {
	t.Helper()
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", status, body)
	}
	env := checkoutError(t, body)
	if env.Error.Code != "plugin.metaldogs.validation_unavailable" || env.Error.Message != "Checkout validation unavailable" {
		t.Fatalf("code=%q message=%q body=%s", env.Error.Code, env.Error.Message, body)
	}
	if env.Error.Details.Retryable == nil || !*env.Error.Details.Retryable {
		t.Fatalf("retryable=%v body=%s", env.Error.Details.Retryable, body)
	}
	if len(env.Error.Details.Lines) != 0 {
		t.Fatalf("lines=%v body=%s", env.Error.Details.Lines, body)
	}
	if secret != "" && bytes.Contains(body, []byte(secret)) {
		t.Fatalf("internal error leaked: %s", body)
	}
	if bytes.Contains(body, []byte("checkout: step")) {
		t.Fatalf("workflow wrap leaked: %s", body)
	}
}

type checkoutErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details struct {
			Lines     []refusedLine `json:"lines"`
			Retryable *bool         `json:"retryable"`
		} `json:"details"`
	} `json:"error"`
}

func checkoutError(t *testing.T, body []byte) checkoutErrorBody {
	t.Helper()
	var env checkoutErrorBody
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("error body: %v %s", err, body)
	}
	return env
}

func assertRefusalLogged(t *testing.T, buf *bytes.Buffer, cartID string, variantIDs, codes []string, absent ...string) {
	t.Helper()
	var matched int
	for _, line := range strings.Split(buf.String(), "\n") {
		if line == "" || !strings.Contains(line, "metaldogs.checkout.design_refused") {
			continue
		}
		var entry struct {
			Event   string         `json:"event"`
			Context map[string]any `json:"context"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line: %v %s", err, line)
		}
		if entry.Context["cart_id"] != cartID {
			continue
		}
		matched++
		if got := stringList(t, entry.Context["variant_ids"]); !sameStrings(got, variantIDs) {
			t.Fatalf("variant_ids=%v want %v line=%s", got, variantIDs, line)
		}
		if got := stringList(t, entry.Context["codes"]); !sameStrings(got, codes) {
			t.Fatalf("codes=%v want %v line=%s", got, codes, line)
		}
		if _, ok := entry.Context["customer_id"]; ok {
			t.Fatalf("customer_id logged: %s", line)
		}
		for _, secret := range absent {
			if secret != "" && strings.Contains(line, secret) {
				t.Fatalf("log contains %q: %s", secret, line)
			}
		}
	}
	if matched != 1 {
		t.Fatalf("refusal log lines for %s = %d\n%s", cartID, matched, buf.String())
	}
}

func stringList(t *testing.T, raw any) []string {
	t.Helper()
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("list = %#v", raw)
	}
	out := make([]string, len(items))
	for i, item := range items {
		out[i], ok = item.(string)
		if !ok {
			t.Fatalf("item %d = %#v", i, item)
		}
	}
	return out
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type checkoutOrderItem struct {
	VariantID  string `json:"variant_id"`
	Extensions []struct {
		FieldCode string `json:"field_code"`
		Value     any    `json:"value"`
	} `json:"extensions"`
}

func checkoutOrder(t *testing.T, body []byte) (string, []checkoutOrderItem) {
	t.Helper()
	var env struct {
		Data struct {
			Order struct {
				ID    string              `json:"id"`
				Items []checkoutOrderItem `json:"items"`
			} `json:"order"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("order body: %v %s", err, body)
	}
	if env.Data.Order.ID == "" {
		t.Fatalf("order id empty: %s", body)
	}
	return env.Data.Order.ID, env.Data.Order.Items
}

func orderVariantIDs(t *testing.T, body []byte) []string {
	t.Helper()
	_, items := checkoutOrder(t, body)
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.VariantID
	}
	return out
}

func assertOrderCarriesDesign(t *testing.T, body []byte, designID string, variantIDs ...string) {
	t.Helper()
	_, items := checkoutOrder(t, body)
	if len(items) != len(variantIDs) {
		t.Fatalf("items=%d want %d body=%s", len(items), len(variantIDs), body)
	}
	seen := map[string]int{}
	for _, item := range items {
		seen[item.VariantID]++
		got := ""
		for _, ext := range item.Extensions {
			if ext.FieldCode != ports.DesignIDFieldCode {
				continue
			}
			value, ok := ext.Value.(string)
			if !ok {
				t.Fatalf("variant %s design id %#v body=%s", item.VariantID, ext.Value, body)
			}
			got = value
		}
		if got != designID {
			t.Fatalf("variant %s design_id=%q want %q body=%s", item.VariantID, got, designID, body)
		}
	}
	for _, id := range variantIDs {
		if seen[id] != 1 {
			t.Fatalf("variant %s count=%d body=%s", id, seen[id], body)
		}
	}
}

type saleSnap struct {
	orders   int
	stock    map[string]int
	reserved map[string]int
}

func takeSaleSnap(t *testing.T, db *sql.DB, variantIDs ...string) saleSnap {
	t.Helper()
	snap := saleSnap{stock: map[string]int{}, reserved: map[string]int{}}
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM orders`).Scan(&snap.orders); err != nil {
		t.Fatal(err)
	}
	for _, id := range variantIDs {
		var qty, reserved int
		if err := db.QueryRowContext(context.Background(), `SELECT quantity FROM stock WHERE variant_id = $1`, id).Scan(&qty); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM reservations WHERE variant_id = $1`, id).Scan(&reserved); err != nil {
			t.Fatal(err)
		}
		snap.stock[id] = qty
		snap.reserved[id] = reserved
	}
	return snap
}

func assertSaleUnchanged(t *testing.T, db *sql.DB, before saleSnap, variantIDs ...string) {
	t.Helper()
	after := takeSaleSnap(t, db, variantIDs...)
	if after.orders != before.orders {
		t.Fatalf("orders %d, want %d", after.orders, before.orders)
	}
	for _, id := range variantIDs {
		if after.stock[id] != before.stock[id] || after.reserved[id] != before.reserved[id] {
			t.Fatalf("variant %s stock %d reserved %d, want stock %d reserved %d", id, after.stock[id], after.reserved[id], before.stock[id], before.reserved[id])
		}
	}
}
