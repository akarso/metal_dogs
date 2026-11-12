package metaldogs_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/domain/tax"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs"
	mdcatalog "github.com/akarso/shopanda/plugins/metaldogs/catalog"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

const catalogPLNHost = "pl.metal-dogs.test"

func TestCatalogSeed_StorefrontHappyPath(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "catalog_seed")
	runCatalogSeed(t, db, repoCatalogDir(t))

	srv := newCatalogStorefront(t, db)
	if products := listCatalogProducts(t, srv); len(products) != 0 {
		t.Fatalf("draft catalog visible: %d products", len(products))
	}
	if status := catalogGET(t, srv, "/api/v1/products/mdogs-tee"); status != http.StatusNotFound {
		t.Fatalf("draft product status = %d, want 404", status)
	}

	publishCatalogProduct(t, db)

	products := listCatalogProducts(t, srv)
	if len(products) != 1 {
		t.Fatalf("products = %d, want 1", len(products))
	}
	product := products[0]
	if product.Name != "Metal Dogs custom tee" || product.Slug != "mdogs-tee" || product.Type != "configurable" || product.Status != "active" {
		t.Fatalf("product = %+v", product)
	}
	if product.Attributes["parent_sku"] != "MDOGS-TEE" {
		t.Fatalf("parent_sku = %#v", product.Attributes["parent_sku"])
	}

	before := listCatalogVariants(t, srv, product.ID)
	assertTeeVariants(t, before, 8049)

	runCatalogSeed(t, db, repoCatalogDir(t))

	afterProducts := listCatalogProducts(t, srv)
	if len(afterProducts) != 1 || afterProducts[0].ID != product.ID || afterProducts[0].Status != "active" {
		t.Fatalf("after second run = %+v", afterProducts)
	}
	if product.UpdatedAt == "" || afterProducts[0].UpdatedAt != product.UpdatedAt {
		t.Fatalf("product UpdatedAt = %q, want %q", afterProducts[0].UpdatedAt, product.UpdatedAt)
	}
	after := listCatalogVariants(t, srv, product.ID)
	if len(after.byID) != len(before.byID) {
		t.Fatalf("variants = %d, want %d", len(after.byID), len(before.byID))
	}
	for id, variant := range before.byID {
		got, ok := after.byID[id]
		if !ok || got.SKU != variant.SKU {
			t.Fatalf("variant %s changed: %+v", id, got)
		}
		if got.UpdatedAt == "" || got.UpdatedAt != variant.UpdatedAt {
			t.Fatalf("variant %s UpdatedAt = %q, want %q", id, got.UpdatedAt, variant.UpdatedAt)
		}
		if after.offers[id] != before.offers[id] {
			t.Fatalf("offer %s = %+v, want %+v", id, after.offers[id], before.offers[id])
		}
	}
}

func TestCatalogSeed_ParentSKUMismatch(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "catalog_seed_parent")
	dir := repoCatalogDir(t)
	runCatalogSeed(t, db, dir)

	repo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	product, err := repo.FindBySlug(ctx, "mdogs-tee")
	if err != nil {
		t.Fatal(err)
	}
	if product == nil {
		t.Fatal("product mdogs-tee missing")
	}
	product.Attributes["parent_sku"] = "OTHER"
	if err := repo.Update(ctx, product); err != nil {
		t.Fatal(err)
	}

	err = runCatalogSeedErr(t, db, dir)
	if err == nil || !strings.Contains(err.Error(), "mdogs-tee") || !strings.Contains(err.Error(), "OTHER") || !strings.Contains(err.Error(), "MDOGS-TEE") {
		t.Fatalf("err = %v", err)
	}

	publishCatalogProduct(t, db)
	srv := newCatalogStorefront(t, db)
	products := listCatalogProducts(t, srv)
	if len(products) != 1 || products[0].Attributes["parent_sku"] != "OTHER" {
		t.Fatalf("parent_sku after mismatch = %+v", products)
	}
}

func TestCatalogSeed_DotDecimal(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "catalog_seed_dot")
	dir := copyCatalogDir(t, repoCatalogDir(t))
	prices := filepath.Join(dir, "prices.csv")
	body, err := os.ReadFile(prices)
	if err != nil {
		t.Fatal(err)
	}
	replaced := strings.ReplaceAll(string(body), `"80,49"`, "80.49")
	if replaced == string(body) {
		t.Fatal("prices.csv has no quoted 80,49 placeholder")
	}
	if err := os.WriteFile(prices, []byte(replaced), 0o644); err != nil {
		t.Fatal(err)
	}
	runCatalogSeed(t, db, dir)
	publishCatalogProduct(t, db)
	srv := newCatalogStorefront(t, db)
	products := listCatalogProducts(t, srv)
	if len(products) != 1 {
		t.Fatalf("products = %d, want 1", len(products))
	}
	assertTeeVariants(t, listCatalogVariants(t, srv, products[0].ID), 8049)
}

func TestCatalogSeed_ProductRepoWithoutWithTxWritesNothing(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "catalog_seed_notx")
	err := runCatalogSeedErrRepos(t, db, repoCatalogDir(t), func(db *sql.DB) (mdcatalog.Repos, error) {
		repos, err := openCatalogRepos(db)
		if err != nil {
			return mdcatalog.Repos{}, err
		}
		repos.Products = productRepoNoTx{ProductRepository: repos.Products}
		return repos, nil
	})
	if err == nil || !strings.Contains(err.Error(), "does not support WithTx") {
		t.Fatalf("err = %v", err)
	}

	activateCatalogProduct(t, db)
	srv := newCatalogStorefront(t, db)
	if products := listCatalogProducts(t, srv); len(products) != 0 {
		t.Fatalf("import without WithTx visible: %+v", products)
	}
	if status := catalogGET(t, srv, "/api/v1/products/mdogs-tee"); status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

func TestCatalogSeed_ExistingProductMissingParentSKU(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "catalog_seed_parent_existing")
	dir := repoCatalogDir(t)
	runCatalogSeed(t, db, dir)

	repo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	product, err := repo.FindBySlug(ctx, "mdogs-tee")
	if err != nil {
		t.Fatal(err)
	}
	if product == nil {
		t.Fatal("product mdogs-tee missing")
	}
	delete(product.Attributes, "parent_sku")
	if err := repo.Update(ctx, product); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM variants WHERE sku = $1`, "MDOGS-TEE-XXL"); err != nil {
		t.Fatal(err)
	}

	runCatalogSeed(t, db, dir)
	activateCatalogProduct(t, db)

	srv := newCatalogStorefront(t, db)
	products := listCatalogProducts(t, srv)
	if len(products) != 1 || products[0].Attributes["parent_sku"] != "MDOGS-TEE" {
		t.Fatalf("product = %+v", products)
	}
	if products[0].CreatedAt == "" || products[0].CreatedAt == products[0].UpdatedAt {
		t.Fatalf("existing product was recreated: created %s updated %s", products[0].CreatedAt, products[0].UpdatedAt)
	}
	assertTeeVariants(t, listCatalogVariants(t, srv, products[0].ID), 8049)
}

func TestCatalogSeed_ParentSKUWrittenWithProduct(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "catalog_seed_parent_insert")
	runCatalogSeed(t, db, repoCatalogDir(t))
	activateCatalogProduct(t, db)

	srv := newCatalogStorefront(t, db)
	products := listCatalogProducts(t, srv)
	if len(products) != 1 || products[0].Attributes["parent_sku"] != "MDOGS-TEE" {
		t.Fatalf("product = %+v", products)
	}
	if products[0].CreatedAt == "" || products[0].CreatedAt != products[0].UpdatedAt {
		t.Fatalf("parent_sku written after insert: created %s updated %s", products[0].CreatedAt, products[0].UpdatedAt)
	}
}

func TestCatalogSeed_ThousandsPrices(t *testing.T) {
	for _, amount := range []string{"1.234,56", "1,234.56"} {
		t.Run(amount, func(t *testing.T) {
			db := testutil.OpenIsolatedWithCore(t, "catalog_seed_thousands")
			dir := copyCatalogDir(t, repoCatalogDir(t))
			setCatalogAmounts(t, dir, func(string) (string, bool) { return amount, true })
			runCatalogSeed(t, db, dir)
			publishCatalogProduct(t, db)
			srv := newCatalogStorefront(t, db)
			products := listCatalogProducts(t, srv)
			if len(products) != 1 || products[0].Attributes["parent_sku"] != "MDOGS-TEE" {
				t.Fatalf("product = %+v", products)
			}
			assertTeeVariants(t, listCatalogVariants(t, srv, products[0].ID), 123456)
		})
	}
}

func TestCatalogSeed_BadPriceWritesNothing(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "catalog_seed_bad_price")
	seeded := copyCatalogDir(t, repoCatalogDir(t))
	setCatalogAmounts(t, seeded, func(string) (string, bool) { return "90,00", true })
	runCatalogSeed(t, db, seeded)
	publishCatalogProduct(t, db)
	srv := newCatalogStorefront(t, db)
	beforeProducts := listCatalogProducts(t, srv)
	if len(beforeProducts) != 1 {
		t.Fatalf("products = %d, want 1", len(beforeProducts))
	}
	before := listCatalogVariants(t, srv, beforeProducts[0].ID)
	assertTeeVariants(t, before, 9000)

	const sku = "MDOGS-TEE-L"
	for _, amount := range []string{"80,499", "80 zł", "80 PLN", "-5", "0", "0,00", "", "abc", "1.234"} {
		t.Run(amount, func(t *testing.T) {
			dir := copyCatalogDir(t, repoCatalogDir(t))
			setCatalogAmounts(t, dir, func(rowSKU string) (string, bool) {
				if rowSKU == sku {
					return amount, true
				}
				return "", false
			})
			err := runCatalogSeedErr(t, db, dir)
			if err == nil {
				t.Fatal("expected non-zero command result")
			}
			msg := err.Error()
			quoted := amount
			if amount == "" {
				quoted = `invalid amount ""`
			}
			for _, part := range []string{"prices.csv", "row 4", "column amount", quoted} {
				if !strings.Contains(msg, part) {
					t.Fatalf("error %q missing %q", msg, part)
				}
			}
			afterProducts := listCatalogProducts(t, srv)
			if len(afterProducts) != 1 || afterProducts[0].ID != beforeProducts[0].ID || afterProducts[0].UpdatedAt != beforeProducts[0].UpdatedAt {
				t.Fatalf("product changed: %+v", afterProducts)
			}
			if afterProducts[0].Attributes["parent_sku"] != "MDOGS-TEE" {
				t.Fatalf("parent_sku = %#v", afterProducts[0].Attributes["parent_sku"])
			}
			after := listCatalogVariants(t, srv, beforeProducts[0].ID)
			if len(after.byID) != len(before.byID) {
				t.Fatalf("variants = %d, want %d", len(after.byID), len(before.byID))
			}
			for id, variant := range before.byID {
				got, ok := after.byID[id]
				if !ok || got.SKU != variant.SKU || got.UpdatedAt != variant.UpdatedAt {
					t.Fatalf("variant %s changed: %+v", id, got)
				}
				if after.offers[id] != before.offers[id] {
					t.Fatalf("offer %s = %+v, want %+v", id, after.offers[id], before.offers[id])
				}
			}
		})
	}
}

func TestCatalogSeed_BadPriceOnEmptyDBWritesNothing(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "catalog_seed_bad_empty")
	dir := copyCatalogDir(t, repoCatalogDir(t))
	setCatalogAmounts(t, dir, func(rowSKU string) (string, bool) {
		if rowSKU == "MDOGS-TEE-L" {
			return "80,499", true
		}
		return "", false
	})
	err := runCatalogSeedErr(t, db, dir)
	if err == nil {
		t.Fatal("expected non-zero command result")
	}
	msg := err.Error()
	for _, part := range []string{"prices.csv", "row 4", "column amount", "80,499"} {
		if !strings.Contains(msg, part) {
			t.Fatalf("error %q missing %q", msg, part)
		}
	}

	activateCatalogProduct(t, db)
	srv := newCatalogStorefront(t, db)
	if products := listCatalogProducts(t, srv); len(products) != 0 {
		t.Fatalf("failed import visible: %+v", products)
	}
	if status := catalogGET(t, srv, "/api/v1/products/mdogs-tee"); status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}

	good := copyCatalogDir(t, repoCatalogDir(t))
	setCatalogAmounts(t, good, func(string) (string, bool) { return "90,00", true })
	runCatalogSeed(t, db, good)
	activateCatalogProduct(t, db)

	products := listCatalogProducts(t, srv)
	if len(products) != 1 || products[0].Attributes["parent_sku"] != "MDOGS-TEE" {
		t.Fatalf("product = %+v", products)
	}
	if products[0].CreatedAt == "" || products[0].CreatedAt != products[0].UpdatedAt {
		t.Fatalf("product existed before the good seed: created %s updated %s", products[0].CreatedAt, products[0].UpdatedAt)
	}
	assertTeeVariants(t, listCatalogVariants(t, srv, products[0].ID), 9000)
}

func runCatalogSeed(t *testing.T, db *sql.DB, dir string) {
	t.Helper()
	if err := runCatalogSeedErr(t, db, dir); err != nil {
		t.Fatal(err)
	}
}

func runCatalogSeedErr(t *testing.T, db *sql.DB, dir string) error {
	t.Helper()
	return runCatalogSeedErrRepos(t, db, dir, openCatalogRepos)
}

func runCatalogSeedErrRepos(t *testing.T, db *sql.DB, dir string, open func(*sql.DB) (mdcatalog.Repos, error)) error {
	t.Helper()
	app := testApp(t, true)
	reg := plugin.NewRegistry(app.Logger)
	plug := metaldogs.New()
	plug.UseCatalogRepos(open)
	reg.Register(plug)
	if summary := reg.InitAll(app); summary.Failed > 0 {
		t.Fatalf("InitAll: %+v", summary)
	}
	cmd, found := reg.CLIRegistry().Get(metaldogs.CommandCatalogSeed)
	if !found {
		t.Fatal("metaldogs:catalog-seed not registered")
	}
	if !cmd.RequiresDB {
		t.Fatal("metaldogs:catalog-seed must require a database")
	}
	return cmd.Run(cli.Context{
		Ctx:    context.Background(),
		DB:     db,
		Logger: app.Logger,
		Config: app.Config,
	}, []string{"-dir", dir})
}

// ProductRepo.Update rewrites updated_at, which would hide a later parent_sku write.
func activateCatalogProduct(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `UPDATE products SET status = 'active' WHERE slug = $1`, "mdogs-tee"); err != nil {
		t.Fatal(err)
	}
}

func publishCatalogProduct(t *testing.T, db *sql.DB) {
	t.Helper()
	repo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	product, err := repo.FindBySlug(ctx, "mdogs-tee")
	if err != nil {
		t.Fatal(err)
	}
	if product == nil {
		t.Fatal("product mdogs-tee missing")
	}
	product.Status = catalog.StatusActive
	if err := repo.Update(ctx, product); err != nil {
		t.Fatal(err)
	}
}

func newCatalogStorefront(t *testing.T, db *sql.DB) *httptest.Server {
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
	stores, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	seedPLNStore(t, stores)
	srv := httptest.NewServer(httpshared.StoreMiddleware(stores, logger.NewWithWriter(io.Discard, "error"))(mux))
	t.Cleanup(srv.Close)
	return srv
}

func seedPLNStore(t *testing.T, stores *postgres.StoreRepo) {
	t.Helper()
	s, err := store.NewStore(id.New(), "pl", "Poland", "PLN", "PL", "pl", catalogPLNHost)
	if err != nil {
		t.Fatal(err)
	}
	if err := stores.Create(context.Background(), &s); err != nil {
		t.Fatal(err)
	}
}

// Embedding the interface drops ProductRepo.WithTx from the method set.
type productRepoNoTx struct {
	catalog.ProductRepository
}

func openCatalogRepos(db *sql.DB) (mdcatalog.Repos, error) {
	products, err := postgres.NewProductRepo(db)
	if err != nil {
		return mdcatalog.Repos{}, err
	}
	variants, err := postgres.NewVariantRepo(db)
	if err != nil {
		return mdcatalog.Repos{}, err
	}
	prices, err := postgres.NewPriceRepo(db)
	if err != nil {
		return mdcatalog.Repos{}, err
	}
	history, err := postgres.NewPriceHistoryRepo(db)
	if err != nil {
		return mdcatalog.Repos{}, err
	}
	stock, err := postgres.NewStockRepo(db)
	if err != nil {
		return mdcatalog.Repos{}, err
	}
	return mdcatalog.Repos{
		Products: products,
		Variants: variants,
		Prices:   prices,
		History:  history,
		Stock:    stock,
		DB:       db,
	}, nil
}

type catalogProduct struct {
	ID         string
	Name       string
	Slug       string
	Status     string
	Type       string
	CreatedAt  string
	UpdatedAt  string
	Attributes map[string]interface{}
}

type catalogVariant struct {
	SKU        string
	UpdatedAt  string
	Attributes map[string]interface{}
}

type catalogOffer struct {
	Amount   int64
	Currency string
	InStock  bool
}

type catalogVariantPage struct {
	byID   map[string]catalogVariant
	offers map[string]catalogOffer
}

func listCatalogProducts(t *testing.T, srv *httptest.Server) []catalogProduct {
	t.Helper()
	body := catalogBody(t, srv, "/api/v1/products", http.StatusOK)
	var payload struct {
		Products []map[string]interface{} `json:"products"`
	}
	decodeData(t, body, &payload)
	out := make([]catalogProduct, 0, len(payload.Products))
	for _, raw := range payload.Products {
		attrs, _ := raw["Attributes"].(map[string]interface{})
		out = append(out, catalogProduct{
			ID:         stringField(raw, "ID"),
			Name:       stringField(raw, "Name"),
			Slug:       stringField(raw, "Slug"),
			Status:     stringField(raw, "Status"),
			Type:       stringField(raw, "Type"),
			CreatedAt:  stringField(raw, "CreatedAt"),
			UpdatedAt:  stringField(raw, "UpdatedAt"),
			Attributes: attrs,
		})
	}
	return out
}

func listCatalogVariants(t *testing.T, srv *httptest.Server, productID string) catalogVariantPage {
	t.Helper()
	body := catalogBody(t, srv, "/api/v1/products/"+productID+"/variants", http.StatusOK)
	var payload struct {
		Variants []struct {
			ID         string                 `json:"ID"`
			SKU        string                 `json:"SKU"`
			UpdatedAt  string                 `json:"UpdatedAt"`
			Attributes map[string]interface{} `json:"Attributes"`
		} `json:"variants"`
		Offers map[string]struct {
			Amount   *int64 `json:"amount"`
			Currency string `json:"currency"`
			InStock  bool   `json:"in_stock"`
		} `json:"offers"`
	}
	decodeData(t, body, &payload)
	page := catalogVariantPage{
		byID:   map[string]catalogVariant{},
		offers: map[string]catalogOffer{},
	}
	for _, variant := range payload.Variants {
		page.byID[variant.ID] = catalogVariant{SKU: variant.SKU, UpdatedAt: variant.UpdatedAt, Attributes: variant.Attributes}
		offer := payload.Offers[variant.ID]
		if offer.Amount == nil {
			t.Fatalf("sku %s has no offer amount", variant.SKU)
		}
		page.offers[variant.ID] = catalogOffer{
			Amount:   *offer.Amount,
			Currency: offer.Currency,
			InStock:  offer.InStock,
		}
	}
	return page
}

func assertTeeVariants(t *testing.T, page catalogVariantPage, netMinor int64) {
	t.Helper()
	want := map[string]string{
		"MDOGS-TEE-S":   "S",
		"MDOGS-TEE-M":   "M",
		"MDOGS-TEE-L":   "L",
		"MDOGS-TEE-XL":  "XL",
		"MDOGS-TEE-XXL": "XXL",
	}
	if len(page.byID) != len(want) {
		t.Fatalf("variants = %d, want %d", len(page.byID), len(want))
	}
	seen := map[string]struct{}{}
	for id, variant := range page.byID {
		size, ok := want[variant.SKU]
		if !ok {
			t.Fatalf("unexpected sku %s", variant.SKU)
		}
		if variant.Attributes["size"] != size {
			t.Fatalf("sku %s size = %#v", variant.SKU, variant.Attributes["size"])
		}
		offer := page.offers[id]
		if offer.Amount != netMinor || offer.Currency != "PLN" || offer.InStock {
			t.Fatalf("sku %s offer = %+v", variant.SKU, offer)
		}
		seen[variant.SKU] = struct{}{}
		if netMinor == 8049 {
			assertPlaceholderGross(t, offer.Amount)
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("skus = %v", seen)
	}
}

func assertPlaceholderGross(t *testing.T, netMinor int64) {
	t.Helper()
	net, err := shared.NewMoney(netMinor, "PLN")
	if err != nil {
		t.Fatal(err)
	}
	// Shop seed standard rate is 2300 basis points (23%). Stored amounts are net.
	vat, err := tax.Calculate(net, tax.TaxRate{Class: tax.TaxClassStandard, Rate: 2300}, tax.ModeExclusive)
	if err != nil {
		t.Fatal(err)
	}
	gross := net.Amount() + vat.Amount()
	if gross != 9900 {
		t.Fatalf("gross = %d, want 9900 (net %d)", gross, netMinor)
	}
}

func catalogGET(t *testing.T, srv *httptest.Server, path string) int {
	t.Helper()
	resp, err := catalogRequest(t, srv, path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func catalogBody(t *testing.T, srv *httptest.Server, path string, want int) []byte {
	t.Helper()
	resp, err := catalogRequest(t, srv, path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s status = %d, body = %s", path, resp.StatusCode, body)
	}
	return body
}

func catalogRequest(t *testing.T, srv *httptest.Server, path string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = catalogPLNHost
	return srv.Client().Do(req)
}

func decodeData(t *testing.T, body []byte, dest interface{}) {
	t.Helper()
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		t.Fatalf("error = %s", envelope.Error)
	}
	if err := json.Unmarshal(envelope.Data, dest); err != nil {
		t.Fatalf("data: %v body=%s", err, body)
	}
}

func stringField(raw map[string]interface{}, key string) string {
	v, _ := raw[key].(string)
	return v
}

func repoCatalogDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "catalog")
}

func setCatalogAmounts(t *testing.T, dir string, amountFor func(sku string) (string, bool)) {
	t.Helper()
	path := filepath.Join(dir, "prices.csv")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(f).ReadAll()
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Fatal("prices.csv empty")
	}
	col := map[string]int{}
	for i, h := range records[0] {
		col[strings.TrimSpace(strings.ToLower(h))] = i
	}
	skuCol, skuOK := col["sku"]
	amountCol, amountOK := col["amount"]
	if !skuOK || !amountOK {
		t.Fatalf("prices.csv header = %v", records[0])
	}
	for i := 1; i < len(records); i++ {
		if skuCol >= len(records[i]) || amountCol >= len(records[i]) {
			t.Fatalf("short price row %d", i+1)
		}
		amount, ok := amountFor(records[i][skuCol])
		if ok {
			records[i][amountCol] = amount
		}
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.WriteAll(records); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyCatalogDir(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".csv") {
			continue
		}
		in, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, entry.Name()), in, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}
