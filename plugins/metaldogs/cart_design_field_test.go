package metaldogs_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestPublicCartAPI_DesignFieldRefusedAndPlainAddWorks(t *testing.T) {
	srv, db := newDesignCartServer(t)
	products := listCatalogProducts(t, srv)
	if len(products) != 1 || products[0].Slug != "mdogs-tee" || products[0].Status != "active" {
		t.Fatalf("catalog = %+v", products)
	}
	page := listCatalogVariants(t, srv, products[0].ID)
	var variantID string
	for id, variant := range page.byID {
		if variant.SKU == "MDOGS-TEE-M" {
			variantID = id
			if !page.offers[id].InStock {
				t.Fatalf("sku %s offer = %+v, want in stock", variant.SKU, page.offers[id])
			}
		}
	}
	if variantID == "" {
		t.Fatal("MDOGS-TEE-M missing from storefront")
	}

	cartID := createPLNCart(t, srv)

	status, body := cartDo(t, srv, http.MethodPost, "/api/v1/carts/"+cartID+"/items", map[string]any{
		"variant_id": variantID,
		"quantity":   1,
		"extensions": []map[string]any{{
			"field_code": "metaldogs.design_id",
			"value":      "11111111-1111-4111-8111-111111111111",
		}},
	})
	if status != http.StatusForbidden || cartErrorCode(t, body) != "forbidden_private_field" {
		t.Fatalf("add with design field: status=%d code=%s body=%s", status, cartErrorCode(t, body), body)
	}
	if items := cartItems(t, srv, cartID); len(items) != 0 {
		t.Fatalf("cart after refused add = %+v, want no line", items)
	}

	status, body = cartDo(t, srv, http.MethodPost, "/api/v1/carts/"+cartID+"/items", map[string]any{
		"variant_id": variantID,
		"quantity":   1,
	})
	if status != http.StatusUnprocessableEntity || cartErrorCode(t, body) != "cart_validation_failed" {
		t.Fatalf("plain add: status=%d code=%s body=%s", status, cartErrorCode(t, body), body)
	}
	issues := validationIssuesFrom(t, body)
	if len(issues) != 1 || !blockingDesignIssue(issues[0], ports.DesignRequiredCode, "design required", variantID) {
		t.Fatalf("plain add issues=%+v", issues)
	}
	if items := cartItems(t, srv, cartID); len(items) != 0 {
		t.Fatalf("plain add stored items=%+v", items)
	}

	design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 4)
	status, body, cookies := addDesignToCart(t, srv, design.ID, token, variantID, nil)
	if status != http.StatusOK {
		t.Fatalf("design add: status=%d body=%s", status, body)
	}
	designCartID := cookieValue(cookies, sessionCartCookie)
	before := cartItems(t, srv, designCartID)
	if len(before) != 1 || storedDesignID(t, db, designCartID, variantID) != design.ID {
		t.Fatalf("design line=%+v stored=%q", before, storedDesignID(t, db, designCartID, variantID))
	}

	status, body = cartDo(t, srv, http.MethodPost, "/api/v1/carts/"+designCartID+"/items", map[string]any{
		"variant_id": variantID,
		"quantity":   1,
		"extensions": []map[string]any{{
			"field_code": "metaldogs.design_id",
			"value":      "22222222-2222-4222-8222-222222222222",
		}},
	})
	if status != http.StatusForbidden || cartErrorCode(t, body) != "forbidden_private_field" {
		t.Fatalf("update with design field: status=%d code=%s body=%s", status, cartErrorCode(t, body), body)
	}
	items := cartItems(t, srv, designCartID)
	if len(items) != 1 || items[0].Quantity != before[0].Quantity || items[0].VariantID != before[0].VariantID || storedDesignID(t, db, designCartID, variantID) != design.ID {
		t.Fatalf("cart after refused update = %+v, want %+v", items, before)
	}
}

type cartLine struct {
	VariantID  string `json:"variant_id"`
	Quantity   int    `json:"quantity"`
	Extensions []struct {
		FieldCode string `json:"field_code"`
		Value     any    `json:"value"`
	} `json:"extensions"`
}

func createPLNCart(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	status, body := cartDo(t, srv, http.MethodPost, "/api/v1/carts", map[string]any{"currency": "PLN"})
	if status != http.StatusCreated {
		t.Fatalf("create cart: status=%d body=%s", status, body)
	}
	var envelope struct {
		Data struct {
			Cart struct {
				ID string `json:"id"`
			} `json:"cart"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Cart.ID == "" {
		t.Fatalf("create cart body=%s", body)
	}
	return envelope.Data.Cart.ID
}

func cartItems(t *testing.T, srv *httptest.Server, cartID string) []cartLine {
	t.Helper()
	status, body := cartDo(t, srv, http.MethodGet, "/api/v1/carts/"+cartID, nil)
	if status != http.StatusOK {
		t.Fatalf("get cart: status=%d body=%s", status, body)
	}
	var envelope struct {
		Data struct {
			Cart struct {
				Items []cartLine `json:"items"`
			} `json:"cart"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("cart body: %v %s", err, body)
	}
	for _, item := range envelope.Data.Cart.Items {
		for _, ext := range item.Extensions {
			if ext.FieldCode == ports.DesignIDFieldCode {
				t.Fatalf("public cart exposes %s: %+v", ports.DesignIDFieldCode, item.Extensions)
			}
		}
	}
	return envelope.Data.Cart.Items
}

func storedDesignID(t *testing.T, db *sql.DB, cartID, variantID string) string {
	t.Helper()
	var value sql.NullString
	err := db.QueryRowContext(context.Background(),
		`SELECT value->>'string_value' FROM extension_values WHERE target_type = 'cart_item' AND target_id = $1 AND field_code = $2`,
		cartID+":"+variantID, ports.DesignIDFieldCode).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("stored design id %s:%s: %v", cartID, variantID, err)
	}
	if !value.Valid {
		return ""
	}
	return value.String
}

func designIDs(t *testing.T, db *sql.DB, cartID string, items []cartLine) map[string]string {
	t.Helper()
	out := make(map[string]string, len(items))
	for _, item := range items {
		out[item.VariantID] = storedDesignID(t, db, cartID, item.VariantID)
	}
	return out
}

func cartErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("error envelope: %v body=%s", err, body)
	}
	return envelope.Error.Code
}
