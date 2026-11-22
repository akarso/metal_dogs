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
	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/application/composition"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/hooks"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/payment"
	domainPricing "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/infrastructure/flatrate"
	"github.com/akarso/shopanda/internal/infrastructure/manualpay"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"
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
	reg.Register(pluginsdk.Adapt(p))
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll summary = %+v", summary)
	}
	return newStorefrontAndCart(t, db, extReg, app), db
}

// No media registration and no Bootstrap.DB, so design services stay unwired.
func newDesignCartServerWithoutMedia(t *testing.T) (*httptest.Server, *sql.DB, *metaldogs.Plugin) {
	t.Helper()
	db := testutil.OpenIsolatedWithCore(t, "cart_design_nomedia")
	runCatalogSeed(t, db, repoCatalogDir(t))
	publishCatalogProduct(t, db)
	setTeeStock(t, db, 5)

	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{Plugins: metalDogsPlugins(true, nil)},
	}
	extReg := extensionapp.NewRegistry()
	app.SetExtensionRegistry(extReg)
	reg := plugin.NewRegistry(log)
	p := metaldogs.New()
	reg.Register(pluginsdk.Adapt(p))
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll summary = %+v", summary)
	}
	if p.DesignService() != nil {
		t.Fatal("design service wired without Bootstrap.DB")
	}
	srv := newStorefrontAndCart(t, db, extReg, app)
	return srv, db, p
}

func setVariantStock(t *testing.T, db *sql.DB, variantID string, qty int) {
	t.Helper()
	stock, err := postgres.NewStockRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := inventory.NewStockEntry(variantID, qty)
	if err != nil {
		t.Fatal(err)
	}
	if err := stock.SetStock(context.Background(), &entry); err != nil {
		t.Fatal(err)
	}
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

func newStorefrontAndCart(t *testing.T, db *sql.DB, extReg *extensionapp.Registry, pluginApp *plugin.App) *httptest.Server {
	t.Helper()
	return openStorefront(t, db, extReg, pluginApp, nil, false)
}

// openStorefront serves the cart API. checkout adds POST /api/v1/checkout with
// the plugin's registered steps, manual payment, and flat-rate shipping.
// cartPort replaces the real cart port when non-nil (plugin must be non-nil).
func openStorefront(t *testing.T, db *sql.DB, extReg *extensionapp.Registry, pluginApp *plugin.App, cartPort any, checkout bool) *httptest.Server {
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
		if err := pluginApp.BindCatalogPort(pluginsdk.NewCatalogPort(products, variants)); err != nil {
			t.Fatal(err)
		}
		boundCart := cartPort
		if boundCart == nil {
			boundCart = pluginsdk.NewCartPort(cartSvc)
		}
		if err := pluginApp.BindCartPort(boundCart); err != nil {
			t.Fatal(err)
		}
	}
	if checkout {
		orders, err := postgres.NewOrderRepo(db)
		if err != nil {
			t.Fatal(err)
		}
		reservations, err := postgres.NewReservationRepo(db)
		if err != nil {
			t.Fatal(err)
		}
		shipments, err := postgres.NewShippingRepo(db)
		if err != nil {
			t.Fatal(err)
		}
		payments, err := postgres.NewPaymentRepo(db)
		if err != nil {
			t.Fatal(err)
		}
		shipReg := shipping.NewProviderRegistry()
		shipReg.Register(flatrate.NewProvider(shared.MustNewMoney(1500, "PLN")))
		payReg := payment.NewProviderRegistry()
		payReg.Register(manualpay.NewProvider())
		createOrder := checkoutApp.NewCreateOrderStep(orders, variants, nil, nil, products, components, prices)
		if pluginApp != nil {
			createOrder = checkoutApp.NewCreateOrderStep(orders, variants, nil, values, products, components, prices)
		}
		coreSteps := []checkoutApp.Step{
			checkoutApp.NewValidateCartStep(variants, products, components, stock),
			checkoutApp.NewRecalculatePricingStep(pipeline),
			checkoutApp.NewReserveInventoryStep(reservations, products, variants, components),
			createOrder,
			checkoutApp.NewSelectShippingStep(shipReg, shipments),
			checkoutApp.NewInitiatePaymentStep(payReg, payments),
		}
		var pluginRegs []checkoutApp.PluginStepRegistration
		if pluginApp != nil {
			for _, reg := range pluginApp.CheckoutStepRegistrations() {
				step, ok := reg.Step.(checkoutApp.Step)
				if !ok {
					t.Fatalf("checkout step %T", reg.Step)
				}
				pluginRegs = append(pluginRegs, checkoutApp.PluginStepRegistration{Step: step, Position: reg.Position})
			}
		}
		steps, err := checkoutApp.MergePluginSteps(coreSteps, pluginRegs)
		if err != nil {
			t.Fatal(err)
		}
		workflow := checkoutApp.NewWorkflow(steps, bus, log)
		checkoutSvc := checkoutApp.NewService(carts, workflow, log)
		mux.Handle("POST /api/v1/checkout", storefront.NewCheckoutHandler(checkoutSvc, extensionapp.AsValueAPI(values)).StartCheckout())
	}

	stores, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	if checkout {
		ensurePLNStore(t, stores)
	} else {
		seedPLNStore(t, stores)
	}
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
