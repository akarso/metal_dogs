package metaldogs_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/composition"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/hooks"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	domainPricing "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/sessioncart"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

var teeSKUs = []string{"MDOGS-TEE-S", "MDOGS-TEE-M", "MDOGS-TEE-L", "MDOGS-TEE-XL", "MDOGS-TEE-XXL"}

// E1 seed stays draft with stock 0, so the harness publishes the tee and sets stock.
func newDesignCartServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.OpenIsolatedWithCore(t, "cart_design")
	runCatalogSeed(t, db, repoCatalogDir(t))
	publishCatalogProduct(t, db)
	setTeeStock(t, db, 5)

	log := logger.NewWithWriter(io.Discard, "error")
	app := testApp(t, true)
	app.Bootstrap = &plugin.Bootstrap{DB: db}
	extReg := extensionapp.NewRegistry()
	app.SetExtensionRegistry(extReg)
	reg := plugin.NewRegistry(log)
	p := metaldogs.New()
	reg.Register(p)
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll summary = %+v", summary)
	}
	return newStorefrontAndCart(t, db, extReg, app, func(carts *cartApp.Service, products catalog.ProductRepository, variants catalog.VariantRepository, lines *extensionapp.ValueService) {
		p.UseSessionCart(sessioncart.New(carts, products, variants, lines, func(ctx context.Context) (context.Context, error) {
			return app.PluginWriteContext(ctx, metaldogs.PluginName, ports.DesignIDFieldCode)
		}))
	}), db
}

func setTeeStock(t *testing.T, db *sql.DB, qty int) {
	t.Helper()
	variants, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	stock, err := postgres.NewStockRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, sku := range teeSKUs {
		variant, err := variants.FindBySKU(ctx, sku)
		if err != nil {
			t.Fatal(err)
		}
		if variant == nil {
			t.Fatalf("sku %s missing after catalog seed", sku)
		}
		entry, err := inventory.NewStockEntry(variant.ID, qty)
		if err != nil {
			t.Fatal(err)
		}
		if err := stock.SetStock(ctx, &entry); err != nil {
			t.Fatal(err)
		}
	}
}

func newStorefrontAndCart(t *testing.T, db *sql.DB, extReg *extensionapp.Registry, pluginApp *plugin.App, bind func(*cartApp.Service, catalog.ProductRepository, catalog.VariantRepository, *extensionapp.ValueService)) *httptest.Server {
	t.Helper()
	products, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	variants, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	prices, err := postgres.NewPriceRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	stock, err := postgres.NewStockRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	components, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	carts, err := postgres.NewCartRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	extValues, err := postgres.NewExtensionValueRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	values := extensionapp.NewValueService(extReg, extValues)
	var hookReg *hooks.Registry
	if pluginApp != nil {
		hookReg = pluginApp.HookRegistry()
	}
	pipeline := domainPricing.NewPipeline(
		appPricing.NewBasePriceStep(appPricing.UnitPriceSources{
			Prices:     prices,
			Variants:   variants,
			Products:   products,
			Components: components,
		}),
		domainPricing.NewFinalizeStep(),
	)
	cartSvc := cartApp.NewService(carts, prices, nil, nil, variants, products, components, stock, pipeline, log, bus, values, hookReg)
	cartHandler := storefront.NewCartHandler(cartSvc, extensionapp.AsValueAPI(values))
	categories, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	pages := storefront.NewStorefrontHandler(
		nil,
		products,
		categories,
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		nil,
	).WithCart(variants, cartSvc)
	productHandler := storefront.NewProductHandler(
		products,
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		nil,
	)
	variantHandler := storefront.NewVariantHandler(products, variants, nil).WithOffers(composition.OfferSources{
		Variants:   variants,
		Prices:     prices,
		Stock:      stock,
		Stocks:     stock,
		Products:   products,
		Components: components,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/products", productHandler.List())
	mux.HandleFunc("GET /api/v1/products/{id}", productHandler.Get())
	mux.HandleFunc("GET /api/v1/products/{id}/variants", variantHandler.List())
	mux.Handle("POST /api/v1/carts", cartHandler.Create())
	mux.Handle("GET /api/v1/carts/{cartId}", cartHandler.Get())
	mux.Handle("POST /api/v1/carts/{cartId}/items", cartHandler.AddItem())
	mux.Handle("PUT /api/v1/carts/{cartId}/items/{variantId}", cartHandler.UpdateItem())
	mux.Handle("DELETE /api/v1/carts/{cartId}/items/{variantId}", cartHandler.RemoveItem())
	mux.HandleFunc("GET /fragments/mini-cart", pages.MiniCartFragment())
	if pluginApp != nil {
		for _, route := range pluginApp.PublicRoutes() {
			mux.Handle(route.Pattern, route.Handler)
		}
	}
	if bind != nil {
		bind(cartSvc, products, variants, values)
	}

	stores, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	seedPLNStore(t, stores)
	srv := httptest.NewServer(httpshared.StoreMiddleware(stores, log)(mux))
	t.Cleanup(srv.Close)
	return srv
}

func cartDo(t *testing.T, srv *httptest.Server, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = catalogPLNHost
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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
	return resp.StatusCode, payload
}
