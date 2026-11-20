package metaldogs

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/pluginwrite"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
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

func TestPlace_ParallelSameVariantSeesOwnPendingDesign(t *testing.T) {
	repo := &recordingDesignRepo{}
	p := New()
	p.designs = &application.DesignService{Designs: repo}
	cart := &pendingValidateCart{plugin: p, enter: make(chan struct{}, 2), goOn: make(chan struct{})}
	p.host = quietHost{
		cart:    cart,
		catalog: fixedTeeCatalog{},
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, designID := range []string{"design-a", "design-b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := p.Place(context.Background(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil), "v", id)
			if err != nil {
				errs <- err
			}
		}(designID)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-cart.enter:
		case <-time.After(2 * time.Second):
			t.Fatal("Place did not reach AddItem")
		}
	}
	close(cart.goOn)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	repo.mu.Lock()
	seen := append([]string(nil), repo.ids...)
	repo.mu.Unlock()
	if len(seen) != 2 || !hasBoth(seen, "design-a", "design-b") {
		t.Fatalf("designs loaded during cart.validate = %v", seen)
	}
}

func hasBoth(ids []string, a, b string) bool {
	var sawA, sawB bool
	for _, id := range ids {
		switch id {
		case a:
			sawA = true
		case b:
			sawB = true
		}
	}
	return sawA && sawB
}

type quietHost struct {
	cart    pluginsdk.CartPort
	catalog pluginsdk.CatalogPort
}

func (h quietHost) Cart() (pluginsdk.CartPort, bool) { return h.cart, h.cart != nil }
func (h quietHost) Catalog() (pluginsdk.CatalogPort, bool) {
	return h.catalog, h.catalog != nil
}
func (quietHost) CatalogSeed() (pluginsdk.CatalogSeedPort, bool) { return nil, false }
func (quietHost) BootstrapDB() (*sql.DB, bool)                   { return nil, false }
func (quietHost) WithPluginWrite(ctx context.Context, pluginName string, fieldCodes ...string) context.Context {
	return pluginwrite.Grant(ctx, pluginName, fieldCodes...)
}
func (quietHost) Logger() pluginsdk.Logger { return nil }

type fixedTeeCatalog struct{}

func (fixedTeeCatalog) FindVariant(ctx context.Context, id string) (*extapi.CatalogVariant, error) {
	found, err := fixedTeeCatalog{}.FindVariants(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	return found[id], nil
}

func (fixedTeeCatalog) FindVariants(_ context.Context, ids []string) (map[string]*extapi.CatalogVariant, error) {
	out := make(map[string]*extapi.CatalogVariant, len(ids))
	for _, id := range ids {
		out[id] = &extapi.CatalogVariant{ID: id, ProductSlug: customTeeSlug, ProductVisible: true}
	}
	return out, nil
}

func (fixedTeeCatalog) FindProduct(context.Context, string) (*extapi.CatalogProduct, error) {
	return nil, nil
}

// Both AddItem calls block until the test releases them, so the two cart.validate hooks overlap.
type pendingValidateCart struct {
	plugin *Plugin
	enter  chan struct{}
	goOn   chan struct{}
}

func (c *pendingValidateCart) AddItem(_ context.Context, cartID, _, variantID string, quantity int, opts pluginsdk.CartAddItemOpts) error {
	c.enter <- struct{}{}
	<-c.goOn
	exts := make([]extapi.CartLineExtension, len(opts.Extensions))
	for i, e := range opts.Extensions {
		exts[i] = extapi.CartLineExtension{Code: e.Code, Value: e.Value}
	}
	hctx := &extapi.HookContext{}
	hctx.Set("cart", &extapi.CartView{
		ID:    cartID,
		Items: []extapi.CartLineView{{VariantID: variantID, Quantity: quantity}},
	})
	hctx.Set("pending_line", &extapi.PendingCartLine{
		VariantID:  variantID,
		Quantity:   quantity,
		Extensions: exts,
	})
	return c.plugin.onCartValidate(hctx)
}

func (c *pendingValidateCart) Get(context.Context, string, string) (*extapi.CartView, error) {
	return &extapi.CartView{ID: "cart-1"}, nil
}

func (c *pendingValidateCart) EnsureActive(context.Context, string, string) (*extapi.CartView, error) {
	return &extapi.CartView{ID: "cart-1"}, nil
}

func (c *pendingValidateCart) EnsureStorefrontCart(context.Context, http.ResponseWriter, *http.Request, string) (*extapi.CartView, error) {
	return &extapi.CartView{ID: "cart-1"}, nil
}

func (c *pendingValidateCart) DiscardEmptyGuestCart(context.Context, http.ResponseWriter, *http.Request, string) error {
	return nil
}

func (c *pendingValidateCart) DeleteLineExtension(context.Context, string, string, string, string) error {
	return nil
}

func (c *pendingValidateCart) UpsertLineExtension(context.Context, string, string, string, []pluginsdk.CartExtensionValue, string) error {
	return nil
}

type recordingDesignRepo struct {
	mu  sync.Mutex
	ids []string
}

func (r *recordingDesignRepo) GetByID(_ context.Context, id string) (domain.Design, bool, error) {
	r.mu.Lock()
	r.ids = append(r.ids, id)
	r.mu.Unlock()
	return domain.Design{}, false, nil
}

func (r *recordingDesignRepo) Insert(context.Context, domain.Design) error { return nil }
func (r *recordingDesignRepo) UpdateEditable(context.Context, domain.Design, string, time.Time, time.Time) error {
	return nil
}
func (r *recordingDesignRepo) CreateBound(context.Context, domain.Design, time.Time) error {
	return nil
}
func (r *recordingDesignRepo) ReplaceSource(context.Context, domain.Design, string, time.Time, time.Time) (string, error) {
	return "", nil
}
