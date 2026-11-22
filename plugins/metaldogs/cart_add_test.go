package metaldogs_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	corepg "github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
)

const sessionCartCookie = "shopanda_storefront_cart"

func TestAddToCart_HappyPathAndAuthorization(t *testing.T) {
	srv, db := newDesignCartServer(t)
	variantM := teeVariantID(t, srv, "MDOGS-TEE-M")
	variantL := teeVariantID(t, srv, "MDOGS-TEE-L")
	approved, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 4)
	before := getDesign(t, srv, approved.ID, token)

	t.Run("guest cart created", func(t *testing.T) {
		status, body, cookies := addDesignToCart(t, srv, approved.ID, token, variantM, nil)
		if status != http.StatusOK {
			t.Fatalf("status=%d body=%s", status, body)
		}
		var resp struct {
			CartURL string `json:"cart_url"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatal(err)
		}
		if resp.CartURL != "/cart" {
			t.Fatalf("cart_url=%q", resp.CartURL)
		}
		if strings.Contains(string(body), "cart_id") {
			t.Fatalf("response exposes cart_id: %s", body)
		}
		cartID := cookieValue(cookies, sessionCartCookie)
		if cartID == "" {
			t.Fatal("session cart cookie missing")
		}
		if strings.Contains(string(body), cartID) {
			t.Fatalf("response contains cart id %s: %s", cartID, body)
		}
		items := cartItems(t, srv, cartID)
		if len(items) != 1 || items[0].VariantID != variantM || items[0].Quantity != 1 {
			t.Fatalf("items=%+v", items)
		}
		if len(items[0].Extensions) != 1 || items[0].Extensions[0].FieldCode != "metaldogs.design_id" || items[0].Extensions[0].Value != approved.ID {
			t.Fatalf("extensions=%+v", items[0].Extensions)
		}
		pageStatus, page := miniCart(t, srv, &http.Cookie{Name: sessionCartCookie, Value: cartID})
		if pageStatus != http.StatusOK {
			t.Fatalf("mini-cart status=%d body=%s", pageStatus, page)
		}
		if !strings.Contains(page, "Metal Dogs custom tee") || strings.Contains(page, "Your cart is empty.") {
			t.Fatalf("mini-cart does not show the line: %s", page)
		}
		after := getDesign(t, srv, approved.ID, token)
		assertDesignUnchanged(t, before, after)
	})

	t.Run("checked-out session cart", func(t *testing.T) {
		oldID := createPLNCart(t, srv)
		markCartCheckedOut(t, db, oldID)
		status, body, cookies := addDesignToCart(t, srv, approved.ID, token, variantM, &http.Cookie{Name: sessionCartCookie, Value: oldID})
		if status != http.StatusOK {
			t.Fatalf("status=%d body=%s", status, body)
		}
		newID := cookieValue(cookies, sessionCartCookie)
		if newID == "" || newID == oldID {
			t.Fatalf("cookie=%q old=%q", newID, oldID)
		}
		if strings.Contains(string(body), newID) || strings.Contains(string(body), oldID) || strings.Contains(string(body), "cart_id") {
			t.Fatalf("response exposes a cart id: %s", body)
		}
		items := cartItems(t, srv, newID)
		if len(items) != 1 || items[0].VariantID != variantM || items[0].Quantity != 1 {
			t.Fatalf("items=%+v", items)
		}
		if len(items[0].Extensions) != 1 || items[0].Extensions[0].Value != approved.ID {
			t.Fatalf("extensions=%+v", items[0].Extensions)
		}
		old := publicCart(t, srv, oldID)
		if old.Status != "checked_out" || len(old.Items) != 0 {
			t.Fatalf("old cart=%+v", old)
		}
		after := getDesign(t, srv, approved.ID, token)
		assertDesignUnchanged(t, before, after)
	})

	t.Run("existing session cart", func(t *testing.T) {
		cartID := createPLNCart(t, srv)
		status, body, cookies := addDesignToCart(t, srv, approved.ID, token, variantL, &http.Cookie{Name: sessionCartCookie, Value: cartID})
		if status != http.StatusOK {
			t.Fatalf("status=%d body=%s", status, body)
		}
		if strings.Contains(string(body), cartID) || strings.Contains(string(body), "cart_id") {
			t.Fatalf("response exposes cart id: %s", body)
		}
		if got := cookieValue(cookies, sessionCartCookie); got != cartID {
			t.Fatalf("cookie=%q, want %q", got, cartID)
		}
		items := cartItems(t, srv, cartID)
		if len(items) != 1 || items[0].VariantID != variantL || items[0].Quantity != 1 {
			t.Fatalf("items=%+v", items)
		}
		if len(items[0].Extensions) != 1 || items[0].Extensions[0].Value != approved.ID {
			t.Fatalf("extensions=%+v", items[0].Extensions)
		}
		after := getDesign(t, srv, approved.ID, token)
		assertDesignUnchanged(t, before, after)
	})

	t.Run("refused", func(t *testing.T) {
		foreign, _, err := application.MintOwnerToken()
		if err != nil {
			t.Fatal(err)
		}
		unknown := id.New()
		expired, expiredToken := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(-time.Hour), 2)
		draft, draftToken := insertCartDesign(t, db, domain.DesignDraft, time.Now().UTC().Add(24*time.Hour), 3)
		ordered, orderedToken := insertCartDesign(t, db, domain.DesignOrdered, time.Now().UTC().Add(24*time.Hour), 9)
		draftBefore := getDesign(t, srv, draft.ID, draftToken)

		cases := []struct {
			name    string
			id      string
			token   string
			query   string
			body    string
			variant string
			want    int
			code    string
		}{
			{name: "missing token", id: approved.ID, token: "", variant: variantM, want: http.StatusNotFound, code: "not_found"},
			{name: "foreign token", id: approved.ID, token: foreign, variant: variantM, want: http.StatusNotFound, code: "not_found"},
			{name: "unknown id", id: unknown, token: token, variant: variantM, want: http.StatusNotFound, code: "not_found"},
			{name: "expired", id: expired.ID, token: expiredToken, variant: variantM, want: http.StatusNotFound, code: "not_found"},
			{name: "not approved", id: draft.ID, token: draftToken, variant: variantM, want: http.StatusConflict, code: "invalid_transition"},
			{name: "ordered", id: ordered.ID, token: orderedToken, variant: variantM, want: http.StatusConflict, code: "invalid_transition"},
			{name: "invalid variant", id: approved.ID, token: token, body: `{"variant_id":"nope"}`, want: http.StatusUnprocessableEntity, code: "unprocessable"},
			{name: "missing variant", id: approved.ID, token: token, body: `{}`, want: http.StatusUnprocessableEntity, code: "unprocessable"},
			{name: "bad json", id: approved.ID, token: token, body: `{`, want: http.StatusUnprocessableEntity, code: "unprocessable"},
			{name: "field from request", id: approved.ID, token: token, body: `{"variant_id":"` + variantM + `","field_code":"metaldogs.design_id"}`, want: http.StatusUnprocessableEntity, code: "unprocessable"},
			{name: "query token", id: approved.ID, token: token, query: "?token=nope", variant: variantM, want: http.StatusBadRequest, code: "bad_request"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				body := tc.body
				if body == "" {
					raw, err := json.Marshal(map[string]string{"variant_id": tc.variant})
					if err != nil {
						t.Fatal(err)
					}
					body = string(raw)
				}
				status, payload, _ := addDesignToCartRaw(t, srv, http.MethodPost, "/api/customizer/designs/"+tc.id+"/add-to-cart"+tc.query, tc.token, body, nil)
				if status != tc.want {
					t.Fatalf("status=%d, want %d body=%s", status, tc.want, payload)
				}
				var errBody struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(payload, &errBody); err != nil {
					t.Fatalf("body=%s", payload)
				}
				if errBody.Error != tc.code {
					t.Fatalf("error=%q, want %q", errBody.Error, tc.code)
				}
			})
		}
		draftAfter := getDesign(t, srv, draft.ID, draftToken)
		assertDesignUnchanged(t, draftBefore, draftAfter)
		approvedAfter := getDesign(t, srv, approved.ID, token)
		assertDesignUnchanged(t, before, approvedAfter)
	})

	t.Run("variant outside the custom tee", func(t *testing.T) {
		other := insertPublishedOtherVariant(t, db)
		assertVariantRefused(t, srv, approved.ID, token, variantM, other)
	})

	t.Run("unknown variant", func(t *testing.T) {
		assertVariantRefused(t, srv, approved.ID, token, variantM, id.New())
	})
}

func assertVariantRefused(t *testing.T, srv *httptest.Server, designID, token, keep, refused string) {
	t.Helper()
	status, body, cookies := addDesignToCart(t, srv, designID, token, keep, nil)
	if status != http.StatusOK {
		t.Fatalf("seed line: status=%d body=%s", status, body)
	}
	cartID := cookieValue(cookies, sessionCartCookie)
	before := cartItems(t, srv, cartID)
	status, payload, _ := addDesignToCart(t, srv, designID, token, refused, &http.Cookie{Name: sessionCartCookie, Value: cartID})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", status, payload)
	}
	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(payload, &errBody); err != nil {
		t.Fatalf("body=%s", payload)
	}
	if errBody.Error != "unprocessable" {
		t.Fatalf("error=%q", errBody.Error)
	}
	after := cartItems(t, srv, cartID)
	if len(before) != len(after) {
		t.Fatalf("cart items %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i].VariantID != after[i].VariantID || before[i].Quantity != after[i].Quantity || len(before[i].Extensions) != len(after[i].Extensions) {
			t.Fatalf("cart changed\nbefore=%+v\nafter=%+v", before, after)
		}
		for j := range before[i].Extensions {
			if before[i].Extensions[j].FieldCode != after[i].Extensions[j].FieldCode || before[i].Extensions[j].Value != after[i].Extensions[j].Value {
				t.Fatalf("extensions changed\nbefore=%+v\nafter=%+v", before[i].Extensions, after[i].Extensions)
			}
		}
	}
}

func insertPublishedOtherVariant(t *testing.T, db *sql.DB) string {
	t.Helper()
	products, err := corepg.NewProductRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	variants, err := corepg.NewVariantRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	product, err := catalog.NewProduct(id.New(), "Other shirt", "other-shirt")
	if err != nil {
		t.Fatal(err)
	}
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	if err := products.Create(ctx, &product); err != nil {
		t.Fatal(err)
	}
	product.Status = catalog.StatusActive
	if err := products.Update(ctx, &product); err != nil {
		t.Fatal(err)
	}
	variant, err := catalog.NewVariant(id.New(), product.ID, "OTHER-SHIRT-M")
	if err != nil {
		t.Fatal(err)
	}
	variant.Name = "M"
	if err := variants.Create(ctx, &variant); err != nil {
		t.Fatal(err)
	}
	stores, err := corepg.NewStoreRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	pl, err := stores.FindByCode(ctx, "pl")
	if err != nil || pl == nil {
		t.Fatalf("pl store: %v", err)
	}
	amount := shared.MustNewMoney(8049, "PLN")
	price, err := pricing.NewPrice(id.New(), variant.ID, pl.ID, amount)
	if err != nil {
		t.Fatal(err)
	}
	prices, err := corepg.NewPriceRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := prices.Upsert(ctx, &price); err != nil {
		t.Fatal(err)
	}
	stock, err := corepg.NewStockRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := inventory.NewStockEntry(variant.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := stock.SetStock(ctx, &entry); err != nil {
		t.Fatal(err)
	}
	return variant.ID
}

func miniCart(t *testing.T, srv *httptest.Server, cookie *http.Cookie) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/fragments/mini-cart", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = catalogPLNHost
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(payload)
}

func teeVariantID(t *testing.T, srv *httptest.Server, sku string) string {
	t.Helper()
	products := listCatalogProducts(t, srv)
	if len(products) != 1 {
		t.Fatalf("products=%d", len(products))
	}
	page := listCatalogVariants(t, srv, products[0].ID)
	for id, variant := range page.byID {
		if variant.SKU == sku {
			return id
		}
	}
	t.Fatalf("sku %s missing", sku)
	return ""
}

func insertCartDesign(t *testing.T, db *sql.DB, status string, expires time.Time, templateVersion int) (domain.Design, string) {
	t.Helper()
	token, hash, err := application.MintOwnerToken()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	d := domain.Design{
		ID:              id.New(),
		TemplateID:      id.New(),
		TemplateVersion: templateVersion,
		Status:          status,
		Texts:           map[string]string{},
		SourceAssetID:   id.New(),
		OwnerTokenHash:  hash,
		CreatedAt:       now,
		UpdatedAt:       now,
		ExpiresAt:       expires.UTC(),
	}
	if err := postgres.NewDesignRepo(db).Insert(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	return d, token
}

func getDesign(t *testing.T, srv *httptest.Server, designID, token string) httpapi.DesignResponse {
	t.Helper()
	status, body, _ := addDesignToCartRaw(t, srv, http.MethodGet, "/api/customizer/designs/"+designID, token, "", nil)
	if status != http.StatusOK {
		t.Fatalf("get design status=%d body=%s", status, body)
	}
	var view httpapi.DesignResponse
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func TestAddToCart_FailedFirstAddLeavesNoGuestCart(t *testing.T) {
	srv, db := newDesignCartServer(t)
	variantM := teeVariantID(t, srv, "MDOGS-TEE-M")
	variantL := teeVariantID(t, srv, "MDOGS-TEE-L")
	approved, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 40)
	draft, draftToken := insertCartDesign(t, db, domain.DesignDraft, time.Now().UTC().Add(24*time.Hour), 41)
	before := cartRowCount(t, db)

	t.Run("draft design refused before Ensure", func(t *testing.T) {
		status, _, cookies := addDesignToCart(t, srv, draft.ID, draftToken, variantM, nil)
		if status != http.StatusConflict {
			t.Fatalf("status=%d", status)
		}
		if id := guestCartCookieID(cookies); id != "" || cartRowCount(t, db) != before {
			t.Fatalf("cookie=%q rows=%d", id, cartRowCount(t, db))
		}
	})

	t.Run("unknown variant refused before Ensure", func(t *testing.T) {
		status, _, cookies := addDesignToCart(t, srv, approved.ID, token, id.New(), nil)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("status=%d", status)
		}
		if id := guestCartCookieID(cookies); id != "" || cartRowCount(t, db) != before {
			t.Fatalf("cookie=%q rows=%d", id, cartRowCount(t, db))
		}
	})

	t.Run("stock failure discards empty guest cart", func(t *testing.T) {
		setTeeStock(t, db, 0)
		status, body, cookies := addDesignToCart(t, srv, approved.ID, token, variantM, nil)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("status=%d body=%s", status, body)
		}
		if id := guestCartCookieID(cookies); id != "" || cartRowCount(t, db) != before {
			t.Fatalf("cookie=%q rows=%d want %d", id, cartRowCount(t, db), before)
		}
	})

	t.Run("stock failure on filled cart leaves it", func(t *testing.T) {
		setTeeStock(t, db, 5)
		status, body, cookies := addDesignToCart(t, srv, approved.ID, token, variantM, nil)
		if status != http.StatusOK {
			t.Fatalf("seed status=%d body=%s", status, body)
		}
		cartID := guestCartCookieID(cookies)
		if cartID == "" {
			t.Fatal("seed cart cookie missing")
		}
		filled := cartItems(t, srv, cartID)
		setTeeStock(t, db, 0)
		status, body, _ = addDesignToCart(t, srv, approved.ID, token, variantL, &http.Cookie{Name: sessionCartCookie, Value: cartID})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("status=%d body=%s", status, body)
		}
		after := cartItems(t, srv, cartID)
		if len(after) != len(filled) || after[0].VariantID != filled[0].VariantID || after[0].Quantity != filled[0].Quantity {
			t.Fatalf("filled cart changed\nbefore=%+v\nafter=%+v", filled, after)
		}
	})
}

func guestCartCookieID(cookies []*http.Cookie) string {
	id := ""
	for _, c := range cookies {
		if c == nil || c.Name != sessionCartCookie {
			continue
		}
		if c.MaxAge < 0 {
			id = ""
			continue
		}
		id = c.Value
	}
	return id
}

func cartRowCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM carts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func assertDesignUnchanged(t *testing.T, before, after httpapi.DesignResponse) {
	t.Helper()
	if after.Status != before.Status || after.TemplateVersion != before.TemplateVersion ||
		!after.ExpiresAt.Equal(before.ExpiresAt) || after.ApprovedVersionID != before.ApprovedVersionID ||
		after.GenerationAttempts != before.GenerationAttempts {
		t.Fatalf("design changed\nbefore=%+v\nafter=%+v", before, after)
	}
}

func addDesignToCart(t *testing.T, srv *httptest.Server, designID, token, variantID string, cookie *http.Cookie) (int, []byte, []*http.Cookie) {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"variant_id": variantID})
	if err != nil {
		t.Fatal(err)
	}
	return addDesignToCartRaw(t, srv, http.MethodPost, "/api/customizer/designs/"+designID+"/add-to-cart", token, string(raw), cookie)
}

func addDesignToCartRaw(t *testing.T, srv *httptest.Server, method, path, token, body string, cookie *http.Cookie) (int, []byte, []*http.Cookie) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = catalogPLNHost
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, payload, resp.Cookies()
}

func markCartCheckedOut(t *testing.T, db *sql.DB, cartID string) {
	t.Helper()
	repo, err := corepg.NewCartRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	current, err := repo.FindByID(ctx, cartID)
	if err != nil || current == nil {
		t.Fatalf("cart %s: %v", cartID, err)
	}
	if err := current.Checkout(); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, current); err != nil {
		t.Fatal(err)
	}
}

type publicCartView struct {
	Status string     `json:"status"`
	Items  []cartLine `json:"items"`
}

func publicCart(t *testing.T, srv *httptest.Server, cartID string) publicCartView {
	t.Helper()
	status, body := cartDo(t, srv, http.MethodGet, "/api/v1/carts/"+cartID, nil)
	if status != http.StatusOK {
		t.Fatalf("get cart: status=%d body=%s", status, body)
	}
	var envelope struct {
		Data struct {
			Cart publicCartView `json:"cart"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("cart body: %v %s", err, body)
	}
	return envelope.Data.Cart
}

func cookieValue(cookies []*http.Cookie, name string) string {
	for _, c := range cookies {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}
