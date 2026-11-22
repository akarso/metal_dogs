package metaldogs_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestCheckout_DesignSale(t *testing.T) {
	db := openCheckoutDB(t)
	enabled, enabledApp := serveCheckout(t, db, true, nil)
	disabled, disabledApp := serveCheckout(t, db, false, nil)
	broken, _ := serveCheckout(t, db, true, struct{}{})
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
		if got := orderVariantIDs(t, body); len(got) != 1 || got[0] != variantM {
			t.Fatalf("variants=%v", got)
		}
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
		got := orderVariantIDs(t, body)
		if len(got) != 2 || !containsAll(got, variantM, variantL) {
			t.Fatalf("variants=%v", got)
		}
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
		if got := orderVariantIDs(t, body); len(got) != 1 || got[0] != variantM {
			t.Fatalf("variants=%v", got)
		}
	})

	t.Run("expired", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 4)
		cartID := addDesignLine(t, enabled, design.ID, token, variantM)
		if _, err := db.ExecContext(context.Background(), `UPDATE metaldogs_designs SET expires_at = now() - interval '1 hour' WHERE id = $1`, design.ID); err != nil {
			t.Fatal(err)
		}
		before := takeSaleSnap(t, db, variantM)
		status, body := postCheckout(t, enabled, cartID)
		assertDesignRefused(t, status, body)
		assertSaleUnchanged(t, db, before, variantM)
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
		assertDesignRefused(t, status, body)
		assertSaleUnchanged(t, db, before, variantM)
	})

	t.Run("draft", func(t *testing.T) {
		refuseStatus(t, db, enabled, variantM, "draft", 6)
	})

	t.Run("preview_ready", func(t *testing.T) {
		refuseStatus(t, db, enabled, variantM, "preview_ready", 7)
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
		assertDesignRefused(t, status, body)
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
		assertDesignRefused(t, status, body)
		assertSaleUnchanged(t, db, before, variantM, variantL)
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
		before := takeSaleSnap(t, db, variantM)
		status, body := postCheckout(t, broken, cartID)
		if status != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", status, body)
		}
		code, message := checkoutError(t, body)
		if code != "internal" || message != "internal server error" {
			t.Fatalf("code=%q message=%q body=%s", code, message, body)
		}
		assertSaleUnchanged(t, db, before, variantM)
	})
}

func refuseStatus(t *testing.T, db *sql.DB, srv *httptest.Server, variantID, status string, version int) {
	t.Helper()
	design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), version)
	cartID := addDesignLine(t, srv, design.ID, token, variantID)
	if _, err := db.ExecContext(context.Background(), `UPDATE metaldogs_designs SET status = $2 WHERE id = $1`, design.ID, status); err != nil {
		t.Fatal(err)
	}
	before := takeSaleSnap(t, db, variantID)
	got, body := postCheckout(t, srv, cartID)
	assertDesignRefused(t, got, body)
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

func serveCheckout(t *testing.T, db *sql.DB, enable bool, cartPort any) (*httptest.Server, *plugin.App) {
	t.Helper()
	app := testApp(t, enable)
	app.Bootstrap = &plugin.Bootstrap{DB: db}
	extReg := extensionapp.NewRegistry()
	app.SetExtensionRegistry(extReg)
	var pluginApp *plugin.App
	if enable {
		reg := plugin.NewRegistry(app.Logger)
		reg.Register(pluginsdk.Adapt(metaldogs.New()))
		summary := reg.InitAll(app)
		if summary.Failed > 0 || summary.Initialized != 1 {
			t.Fatalf("InitAll summary = %+v", summary)
		}
		pluginApp = app
	}
	return openStorefront(t, db, extReg, pluginApp, cartPort, true), pluginApp
}

func postCheckout(t *testing.T, srv *httptest.Server, cartID string) (int, []byte) {
	t.Helper()
	return cartDo(t, srv, http.MethodPost, "/api/v1/checkout", map[string]any{
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
	})
}

func assertDesignRefused(t *testing.T, status int, body []byte) {
	t.Helper()
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", status, body)
	}
	code, message := checkoutError(t, body)
	if code != "validation" || message != "Design no longer available" {
		t.Fatalf("code=%q message=%q body=%s", code, message, body)
	}
}

func checkoutError(t *testing.T, body []byte) (string, string) {
	t.Helper()
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("error body: %v %s", err, body)
	}
	return env.Error.Code, env.Error.Message
}

func orderVariantIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var env struct {
		Data struct {
			Order struct {
				ID    string `json:"id"`
				Items []struct {
					VariantID string `json:"variant_id"`
				} `json:"items"`
			} `json:"order"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("order body: %v %s", err, body)
	}
	if env.Data.Order.ID == "" {
		t.Fatalf("order id empty: %s", body)
	}
	out := make([]string, len(env.Data.Order.Items))
	for i, item := range env.Data.Order.Items {
		out[i] = item.VariantID
	}
	return out
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

func containsAll(got []string, want ...string) bool {
	seen := map[string]int{}
	for _, id := range got {
		seen[id]++
	}
	for _, id := range want {
		if seen[id] == 0 {
			return false
		}
	}
	return true
}
