package metaldogs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestPlace_EmptySlugIsNotTee(t *testing.T) {
	cart := &qtyCart{}
	p := New()
	p.host = &surface{cart: cart, catalog: &slugCatalog{slugs: map[string]string{"v": ""}}}
	_, err := p.Place(context.Background(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil), "v", "design-a")
	var de *application.DesignError
	if !errors.As(err, &de) || de.Code != application.DesignErrUnprocessable {
		t.Fatalf("err=%v", err)
	}
	if cart.ensureCalls != 0 {
		t.Fatal("EnsureStorefrontCart ran for a non-tee")
	}
}
