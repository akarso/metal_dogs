package metaldogs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/pluginwrite"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestPlace_WriteGrantOnlyOnAddItem(t *testing.T) {
	cart := &qtyCart{}
	catalog := &slugCatalog{slugs: map[string]string{"v": customTeeSlug}}
	host := &surface{cart: cart, catalog: catalog}
	p := New()
	p.host = host
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/customizer/designs/x/add-to-cart", nil)
	parent := pluginwrite.Grant(context.Background(), "other", "other.field")
	url, err := p.Place(parent, rec, req, "v", "design-a")
	if err != nil {
		t.Fatal(err)
	}
	if url != "/cart" {
		t.Fatalf("url=%q", url)
	}
	if host.writeCalls != 1 || cart.addCalls != 1 || cart.ensureCalls != 1 || cart.upsertCalls != 0 || cart.deleteCalls != 0 {
		t.Fatalf("writes=%d add=%d ensure=%d upsert=%d delete=%d", host.writeCalls, cart.addCalls, cart.ensureCalls, cart.upsertCalls, cart.deleteCalls)
	}
	if _, ok := pluginwrite.For(cart.ensureCtx, ports.DesignIDFieldCode); ok {
		t.Fatal("EnsureStorefrontCart carried the design write grant")
	}
	if _, ok := pluginwrite.For(catalog.ctx, ports.DesignIDFieldCode); ok {
		t.Fatal("FindVariants carried the design write grant")
	}
	if who, ok := pluginwrite.For(cart.addCtx, ports.DesignIDFieldCode); !ok || who != PluginName {
		t.Fatalf("AddItem grant = %q ok=%v", who, ok)
	}
	if cart.lastOpts.UpdatedBy != "guest" {
		t.Fatalf("UpdatedBy=%q", cart.lastOpts.UpdatedBy)
	}
	if _, ok := pluginwrite.For(cart.addCtx, "other.field"); ok {
		t.Fatal("AddItem kept a grant it did not create")
	}
}

func TestPlace_SameDesignDoesNotWrite(t *testing.T) {
	cart := &qtyCart{ensureView: &extapi.CartView{
		ID: "c1",
		Items: []extapi.CartLineView{{
			VariantID:  "v",
			Quantity:   1,
			Extensions: []extapi.CartLineExtension{{Code: ports.DesignIDFieldCode, Value: "design-a"}},
		}},
	}}
	host := &surface{cart: cart, catalog: &slugCatalog{slugs: map[string]string{"v": customTeeSlug}}}
	p := New()
	p.host = host
	url, err := p.Place(context.Background(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil), "v", "design-a")
	if err != nil {
		t.Fatal(err)
	}
	if url != "/cart" || cart.addCalls != 0 || host.writeCalls != 0 || cart.upsertCalls != 0 {
		t.Fatalf("url=%q add=%d writes=%d upsert=%d", url, cart.addCalls, host.writeCalls, cart.upsertCalls)
	}
}

func TestPlace_OtherDesignConflictsWithoutWrite(t *testing.T) {
	cart := &qtyCart{ensureView: &extapi.CartView{
		ID: "c1",
		Items: []extapi.CartLineView{{
			VariantID:  "v",
			Quantity:   1,
			Extensions: []extapi.CartLineExtension{{Code: ports.DesignIDFieldCode, Value: "design-b"}},
		}},
	}}
	host := &surface{cart: cart, catalog: &slugCatalog{slugs: map[string]string{"v": customTeeSlug}}}
	p := New()
	p.host = host
	_, err := p.Place(context.Background(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil), "v", "design-a")
	var de *application.DesignError
	if !errors.As(err, &de) || de.Code != application.DesignErrConflict {
		t.Fatalf("err=%v", err)
	}
	if cart.addCalls != 0 || host.writeCalls != 0 || cart.upsertCalls != 0 || cart.deleteCalls != 0 {
		t.Fatalf("add=%d writes=%d upsert=%d delete=%d", cart.addCalls, host.writeCalls, cart.upsertCalls, cart.deleteCalls)
	}
}

func TestPlace_UpdatedByFollowsCartCustomer(t *testing.T) {
	cart := &qtyCart{ensureView: &extapi.CartView{ID: "c1", CustomerID: " cust-9 "}}
	p := New()
	p.host = &surface{cart: cart, catalog: &slugCatalog{slugs: map[string]string{"v": customTeeSlug}}}
	if _, err := p.Place(context.Background(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil), "v", "design-a"); err != nil {
		t.Fatal(err)
	}
	if cart.lastOpts.UpdatedBy != "cust-9" {
		t.Fatalf("UpdatedBy=%q", cart.lastOpts.UpdatedBy)
	}
}

func TestPlace_EmptySlugBlocks(t *testing.T) {
	cart := &qtyCart{}
	p := New()
	p.host = &surface{cart: cart, catalog: &slugCatalog{slugs: map[string]string{"v": ""}}}
	_, err := p.Place(context.Background(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil), "v", "design-a")
	var de *application.DesignError
	if !errors.As(err, &de) || de.Code != application.DesignErrUnprocessable || de.Message != "product is not available" {
		t.Fatalf("err=%v", err)
	}
	if cart.ensureCalls != 0 || cart.discardCalls != 0 {
		t.Fatalf("ensure=%d discard=%d", cart.ensureCalls, cart.discardCalls)
	}
}

func TestPlace_OtherProductSkipsTee(t *testing.T) {
	cart := &qtyCart{}
	p := New()
	p.host = &surface{cart: cart, catalog: &slugCatalog{slugs: map[string]string{"v": "other-shirt"}}}
	_, err := p.Place(context.Background(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil), "v", "design-a")
	var de *application.DesignError
	if !errors.As(err, &de) || de.Code != application.DesignErrUnprocessable || de.Message != "variant is not part of the custom tee" {
		t.Fatalf("err=%v", err)
	}
	if cart.ensureCalls != 0 || cart.discardCalls != 0 {
		t.Fatalf("ensure=%d discard=%d", cart.ensureCalls, cart.discardCalls)
	}
}

func TestPlace_HiddenTeeBlocks(t *testing.T) {
	cart := &qtyCart{}
	p := New()
	p.host = &surface{cart: cart, catalog: &slugCatalog{hidden: map[string]string{"v": customTeeSlug}}}
	_, err := p.Place(context.Background(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil), "v", "design-a")
	var de *application.DesignError
	if !errors.As(err, &de) || de.Code != application.DesignErrUnprocessable || de.Message != "product is not available" {
		t.Fatalf("err=%v", err)
	}
	if cart.ensureCalls != 0 {
		t.Fatal("EnsureStorefrontCart ran for an unreadable product")
	}
}

func TestPlace_FailedFirstAddDiscardsEmptyGuestCart(t *testing.T) {
	cart := &qtyCart{addErr: apperror.Validation("no stock")}
	p := New()
	p.host = &surface{cart: cart, catalog: &slugCatalog{slugs: map[string]string{"v": customTeeSlug}}}
	rec := httptest.NewRecorder()
	_, err := p.Place(context.Background(), rec, httptest.NewRequest(http.MethodPost, "/", nil), "v", "design-a")
	var de *application.DesignError
	if !errors.As(err, &de) || de.Code != application.DesignErrUnprocessable {
		t.Fatalf("err=%v", err)
	}
	if cart.discardCalls != 1 || cart.discardID != "cart-1" || cart.ensureCalls != 1 {
		t.Fatalf("discard=%d id=%q ensure=%d", cart.discardCalls, cart.discardID, cart.ensureCalls)
	}
}

func TestPlace_FailedAddOnEmptyCustomerCartDoesNotDiscard(t *testing.T) {
	cart := &qtyCart{
		addErr:     apperror.Validation("no stock"),
		ensureView: &extapi.CartView{ID: "c1", CustomerID: "cust-9"},
	}
	p := New()
	p.host = &surface{cart: cart, catalog: &slugCatalog{slugs: map[string]string{"v": customTeeSlug}}}
	_, err := p.Place(context.Background(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil), "v", "design-a")
	if err == nil {
		t.Fatal("expected add failure")
	}
	if cart.discardCalls != 0 || cart.addCalls != 1 {
		t.Fatalf("discard=%d add=%d", cart.discardCalls, cart.addCalls)
	}
}

func TestPlace_FailedAddOnFilledCartDoesNotDiscard(t *testing.T) {
	cart := &qtyCart{
		addErr: apperror.Validation("no stock"),
		ensureView: &extapi.CartView{
			ID:    "c1",
			Items: []extapi.CartLineView{{VariantID: "shirt", Quantity: 1}},
		},
	}
	p := New()
	p.host = &surface{cart: cart, catalog: &slugCatalog{slugs: map[string]string{"v": customTeeSlug}}}
	_, err := p.Place(context.Background(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil), "v", "design-a")
	if err == nil {
		t.Fatal("expected add failure")
	}
	if cart.discardCalls != 0 || cart.addCalls != 1 {
		t.Fatalf("discard=%d add=%d", cart.discardCalls, cart.addCalls)
	}
}
