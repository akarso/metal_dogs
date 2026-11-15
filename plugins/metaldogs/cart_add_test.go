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
		draftBefore := getDesign(t, srv, draft.ID, draftToken)
		outside := id.New()

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
			{name: "outside tee", id: approved.ID, token: token, variant: outside, want: http.StatusUnprocessableEntity, code: "unprocessable"},
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

func cookieValue(cookies []*http.Cookie, name string) string {
	for _, c := range cookies {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}
