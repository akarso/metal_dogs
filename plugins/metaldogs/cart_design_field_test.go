package metaldogs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicCartAPI_DesignFieldRefusedAndPlainAddWorks(t *testing.T) {
	srv, _ := newDesignCartServer(t)
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
	if status != http.StatusForbidden || cartErrorCode(t, body) != "forbidden_plugin_only_field" {
		t.Fatalf("add with design field: status=%d code=%s body=%s", status, cartErrorCode(t, body), body)
	}
	if items := cartItems(t, srv, cartID); len(items) != 0 {
		t.Fatalf("cart after refused add = %+v, want no line", items)
	}

	status, body = cartDo(t, srv, http.MethodPost, "/api/v1/carts/"+cartID+"/items", map[string]any{
		"variant_id": variantID,
		"quantity":   1,
	})
	if status != http.StatusOK {
		t.Fatalf("plain add: status=%d body=%s", status, body)
	}
	items := cartItems(t, srv, cartID)
	if len(items) != 1 || items[0].VariantID != variantID || items[0].Quantity != 1 {
		t.Fatalf("plain add items = %+v", items)
	}
	if len(items[0].Extensions) != 0 {
		t.Fatalf("plain add stored extensions %+v", items[0].Extensions)
	}
	before := items[0]

	status, body = cartDo(t, srv, http.MethodPost, "/api/v1/carts/"+cartID+"/items", map[string]any{
		"variant_id": variantID,
		"quantity":   1,
		"extensions": []map[string]any{{
			"field_code": "metaldogs.design_id",
			"value":      "22222222-2222-4222-8222-222222222222",
		}},
	})
	if status != http.StatusForbidden || cartErrorCode(t, body) != "forbidden_plugin_only_field" {
		t.Fatalf("update with design field: status=%d code=%s body=%s", status, cartErrorCode(t, body), body)
	}
	items = cartItems(t, srv, cartID)
	if len(items) != 1 || items[0].Quantity != before.Quantity || items[0].VariantID != before.VariantID {
		t.Fatalf("cart after refused update = %+v, want %+v", items, before)
	}
	if len(items[0].Extensions) != 0 {
		t.Fatalf("refused update stored extensions %+v", items[0].Extensions)
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
	return envelope.Data.Cart.Items
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
