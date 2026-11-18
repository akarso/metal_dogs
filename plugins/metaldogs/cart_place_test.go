package metaldogs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/akarso/shopanda/internal/platform/pluginwrite"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
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

func TestPlace_InterleavedSameVariantKeepsOwnDesign(t *testing.T) {
	p := New()
	aIn := make(chan struct{})
	bIn := make(chan struct{})
	aGone := make(chan struct{})
	errs := make(chan error, 4)
	cart := &gateCart{plugin: p, stage: func(design string) {
		switch design {
		case "design-a":
			close(aIn)
			<-bIn
			id, ok := p.pendingDesign("cart-1", "v")
			if !ok || id != "design-a" {
				errs <- fmt.Errorf("A saw %q ok=%v", id, ok)
			}
		case "design-b":
			close(bIn)
			<-aIn
			<-aGone
			id, ok := p.pendingDesign("cart-1", "v")
			if !ok || id != "design-b" {
				errs <- fmt.Errorf("B saw %q ok=%v", id, ok)
			}
		default:
			errs <- fmt.Errorf("design %q", design)
		}
	}}
	p.host = quietHost{cart: cart, catalog: fixedTeeCatalog{}}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := p.Place(context.Background(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil), "v", "design-a"); err != nil {
			errs <- err
		}
		close(aGone)
	}()
	go func() {
		defer wg.Done()
		if _, err := p.Place(context.Background(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil), "v", "design-b"); err != nil {
			errs <- err
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	p.pendingMu.Lock()
	left := len(p.pending)
	p.pendingMu.Unlock()
	if left != 0 {
		t.Fatalf("pending left=%d", left)
	}
}

type quietHost struct {
	cart    pluginsdk.CartPort
	catalog pluginsdk.CatalogPort
}

func (q quietHost) Cart() (pluginsdk.CartPort, bool) { return q.cart, q.cart != nil }
func (q quietHost) Catalog() (pluginsdk.CatalogPort, bool) {
	return q.catalog, q.catalog != nil
}
func (q quietHost) CatalogSeed() (pluginsdk.CatalogSeedPort, bool) { return nil, false }
func (q quietHost) WithPluginWrite(ctx context.Context, pluginName string, fieldCodes ...string) context.Context {
	return pluginwrite.Grant(ctx, pluginName, fieldCodes...)
}
func (q quietHost) Logger() pluginsdk.Logger { return nil }

type fixedTeeCatalog struct{}

func (fixedTeeCatalog) FindVariant(ctx context.Context, id string) (*extapi.CatalogVariant, error) {
	found, err := fixedTeeCatalog{}.FindVariants(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	return found[id], nil
}

func (fixedTeeCatalog) FindVariants(context.Context, []string) (map[string]*extapi.CatalogVariant, error) {
	return map[string]*extapi.CatalogVariant{"v": {ID: "v", ProductSlug: customTeeSlug}}, nil
}

func (fixedTeeCatalog) FindProduct(context.Context, string) (*extapi.CatalogProduct, error) {
	return nil, nil
}

type gateCart struct {
	plugin *Plugin
	stage  func(design string)
}

func (g *gateCart) AddItem(_ context.Context, _, _, variantID string, _ int, opts pluginsdk.CartAddItemOpts) error {
	design, _ := opts.Extensions[0].Value.(string)
	hctx := &extapi.HookContext{}
	hctx.Set("cart_id", "cart-1")
	hctx.Set("variant_id", variantID)
	hctx.Set("extensions", []map[string]interface{}{{
		"field_code": ports.DesignIDFieldCode,
		"value":      design,
	}})
	if err := g.plugin.onAddItemBefore(hctx); err != nil {
		return err
	}
	g.stage(design)
	return nil
}

func (g *gateCart) Get(context.Context, string, string) (*extapi.CartView, error) { return nil, nil }
func (g *gateCart) EnsureActive(context.Context, string, string) (*extapi.CartView, error) {
	return nil, nil
}
func (g *gateCart) EnsureStorefrontCart(context.Context, http.ResponseWriter, *http.Request, string) (*extapi.CartView, error) {
	return &extapi.CartView{ID: "cart-1"}, nil
}
func (g *gateCart) DeleteLineExtension(context.Context, string, string, string, string) error {
	return nil
}
func (g *gateCart) DiscardEmptyGuestCart(context.Context, http.ResponseWriter, *http.Request, string) error {
	return nil
}
func (g *gateCart) UpsertLineExtension(context.Context, string, string, string, []pluginsdk.CartExtensionValue, string) error {
	return nil
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
