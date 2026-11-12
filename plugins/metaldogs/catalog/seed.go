package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/akarso/shopanda/internal/application/importer"
	domcat "github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// Repos are the core catalog ports Seed writes through. DB begins importer transactions.
type Repos struct {
	Products domcat.ProductRepository
	Variants domcat.VariantRepository
	Prices   pricing.PriceRepository
	History  pricing.PriceHistoryRepository
	Stock    inventory.StockRepository
	DB       *sql.DB
}

const (
	productsFile = "products.csv"
	pricesFile   = "prices.csv"
	stockFile    = "stock.csv"

	parentSKUAttribute = "parent_sku"
)

// Seed loads products, prices and stock from dir through the core importers.
// Existing SKUs are skipped and status of an existing product is never changed.
func Seed(ctx context.Context, repos Repos, dir string) error {
	if err := repos.ready(); err != nil {
		return err
	}
	products, prices, stocks, err := loadDir(dir)
	if err != nil {
		return err
	}

	productRepo := repos.Products
	variantRepo := repos.Variants
	priceRepo := repos.Prices
	historyRepo := repos.History
	stockRepo := repos.Stock
	db := repos.DB

	skus := make([]string, len(products))
	for i, row := range products {
		skus[i] = row.sku
	}
	existing, err := variantRepo.FindBySKUs(ctx, skus)
	if err != nil {
		return fmt.Errorf("metaldogs:catalog-seed: find sku: %w", err)
	}
	if existing == nil {
		existing = map[string]*domcat.Variant{}
	}

	bySlug := map[string][]productRow{}
	var slugOrder []string
	for _, row := range products {
		if _, ok := bySlug[row.slug]; !ok {
			slugOrder = append(slugOrder, row.slug)
		}
		bySlug[row.slug] = append(bySlug[row.slug], row)
	}

	opCtx := domcat.WithOperatorProductReadScope(ctx)
	for _, slug := range slugOrder {
		rows := bySlug[slug]
		var fresh []productRow
		for _, row := range rows {
			if existing[row.sku] == nil {
				fresh = append(fresh, row)
			}
		}
		if len(fresh) == 0 {
			if err := ensureParentSKU(opCtx, productRepo, slug, rows[0].parentSKU); err != nil {
				return err
			}
			continue
		}
		stored, err := productRepo.FindBySlug(opCtx, slug)
		if err != nil {
			return fmt.Errorf("metaldogs:catalog-seed: find product %q: %w", slug, err)
		}
		body, err := productCSV(fresh, stored == nil)
		if err != nil {
			return err
		}
		result, err := importer.NewProductImporter(
			productsWithParentSKU(productRepo, map[string]string{slug: rows[0].parentSKU}),
			variantRepo,
			db,
		).Import(ctx, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("metaldogs:catalog-seed: products: %w", err)
		}
		if err := resultErrors("products", result.Errors); err != nil {
			return err
		}
		if err := ensureParentSKU(opCtx, productRepo, slug, rows[0].parentSKU); err != nil {
			return err
		}
	}

	known, err := variantRepo.FindBySKUs(ctx, skus)
	if err != nil {
		return fmt.Errorf("metaldogs:catalog-seed: find sku: %w", err)
	}
	priceBody, err := priceCSV(ctx, priceRepo, known, prices)
	if err != nil {
		return err
	}
	if priceBody != nil {
		result, err := importer.NewPriceImporter(variantRepo, priceRepo, historyRepo, db, nil).
			WithProducts(productRepo).
			Import(ctx, bytes.NewReader(priceBody))
		if err != nil {
			return fmt.Errorf("metaldogs:catalog-seed: prices: %w", err)
		}
		if err := resultErrors("prices", result.Errors); err != nil {
			return err
		}
	}

	stockBody, err := stockCSV(ctx, stockRepo, known, stocks)
	if err != nil {
		return err
	}
	if stockBody != nil {
		result, err := importer.NewStockImporter(variantRepo, stockRepo).
			WithProducts(productRepo).
			WithTxStarter(db).
			Import(ctx, bytes.NewReader(stockBody))
		if err != nil {
			return fmt.Errorf("metaldogs:catalog-seed: stock: %w", err)
		}
		if err := resultErrors("stock", result.Errors); err != nil {
			return err
		}
	}
	return nil
}

func (r Repos) ready() error {
	if r.DB == nil {
		return fmt.Errorf("metaldogs:catalog-seed: database required")
	}
	if r.Products == nil || r.Variants == nil || r.Prices == nil || r.History == nil || r.Stock == nil {
		return fmt.Errorf("metaldogs:catalog-seed: catalog repositories required")
	}
	return nil
}

func resultErrors(kind string, errs []string) error {
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("metaldogs:catalog-seed: %s: %s", kind, strings.Join(errs, "; "))
}

type productTxBinder interface {
	domcat.ProductRepository
	WithTx(tx *sql.Tx) domcat.ProductRepository
}

// parentSKUProducts puts parent_sku on the product insert. The core importer commits that transaction itself.
type parentSKUProducts struct {
	domcat.ProductRepository
	bind   func(*sql.Tx) domcat.ProductRepository
	bySlug map[string]string
}

func productsWithParentSKU(inner domcat.ProductRepository, bySlug map[string]string) domcat.ProductRepository {
	wrapped := parentSKUProducts{ProductRepository: inner, bySlug: bySlug}
	if binder, ok := inner.(productTxBinder); ok {
		wrapped.bind = binder.WithTx
	}
	return wrapped
}

func (p parentSKUProducts) WithTx(tx *sql.Tx) domcat.ProductRepository {
	if p.bind == nil {
		return p
	}
	return parentSKUProducts{ProductRepository: p.bind(tx), bind: p.bind, bySlug: p.bySlug}
}

func (p parentSKUProducts) Create(ctx context.Context, product *domcat.Product) error {
	if product != nil {
		if sku, ok := p.bySlug[product.Slug]; ok {
			if product.Attributes == nil {
				product.Attributes = map[string]interface{}{}
			}
			product.Attributes[parentSKUAttribute] = sku
		}
	}
	return p.ProductRepository.Create(ctx, product)
}

func ensureParentSKU(ctx context.Context, products domcat.ProductRepository, slug, parentSKU string) error {
	product, err := products.FindBySlug(ctx, slug)
	if err != nil {
		return fmt.Errorf("metaldogs:catalog-seed: find product %q: %w", slug, err)
	}
	if product == nil {
		return fmt.Errorf("metaldogs:catalog-seed: product %q not found", slug)
	}
	if product.Attributes == nil {
		product.Attributes = map[string]interface{}{}
	}
	raw, exists := product.Attributes[parentSKUAttribute]
	if exists && raw != nil {
		got, ok := raw.(string)
		if ok && got == parentSKU {
			return nil
		}
		shown := got
		if !ok {
			shown = fmt.Sprint(raw)
		}
		return fmt.Errorf("metaldogs:catalog-seed: product %q parent_sku is %q, catalog file has %q", slug, shown, parentSKU)
	}
	product.Attributes[parentSKUAttribute] = parentSKU
	if err := products.Update(ctx, product); err != nil {
		return fmt.Errorf("metaldogs:catalog-seed: parent sku: %w", err)
	}
	return nil
}

func productCSV(rows []productRow, includeStatus bool) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	header := []string{"name", "slug", "sku", "type", "variant_name", "size"}
	if includeStatus {
		header = []string{"name", "slug", "sku", "type", "status", "variant_name", "size"}
	}
	if err := w.Write(header); err != nil {
		return nil, err
	}
	for _, row := range rows {
		rec := []string{row.name, row.slug, row.sku, row.productType, row.variantName, row.size}
		if includeStatus {
			rec = []string{row.name, row.slug, row.sku, row.productType, row.status, row.variantName, row.size}
		}
		if err := w.Write(rec); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("metaldogs:catalog-seed: products csv: %w", err)
	}
	return buf.Bytes(), nil
}

func priceCSV(ctx context.Context, prices pricing.PriceRepository, variants map[string]*domcat.Variant, rows []priceRow) ([]byte, error) {
	var pending []priceRow
	for _, row := range rows {
		variant := variants[row.sku]
		if variant == nil {
			return nil, fmt.Errorf("metaldogs:catalog-seed: prices: unknown sku %q", row.sku)
		}
		stored, err := prices.FindByVariantCurrencyAndStore(ctx, variant.ID, row.currency, "")
		if err != nil {
			return nil, fmt.Errorf("metaldogs:catalog-seed: prices: %w", err)
		}
		if stored != nil && stored.Amount.Amount() == row.minor && stored.Amount.Currency() == row.currency {
			continue
		}
		pending = append(pending, row)
	}
	if len(pending) == 0 {
		return nil, nil
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"sku", "currency", "amount"}); err != nil {
		return nil, err
	}
	for _, row := range pending {
		if err := w.Write([]string{row.sku, row.currency, strconv.FormatInt(row.minor, 10)}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("metaldogs:catalog-seed: prices csv: %w", err)
	}
	return buf.Bytes(), nil
}

func stockCSV(ctx context.Context, stocks inventory.StockRepository, variants map[string]*domcat.Variant, rows []stockRow) ([]byte, error) {
	var pending []stockRow
	for _, row := range rows {
		variant := variants[row.sku]
		if variant == nil {
			return nil, fmt.Errorf("metaldogs:catalog-seed: stock: unknown sku %q", row.sku)
		}
		entry, err := stocks.GetStock(ctx, variant.ID)
		if err != nil {
			return nil, fmt.Errorf("metaldogs:catalog-seed: stock: %w", err)
		}
		// A missing stock row is also quantity 0; UpdatedAt stays zero only then.
		if !entry.UpdatedAt.IsZero() && entry.Quantity == row.qty {
			continue
		}
		pending = append(pending, row)
	}
	if len(pending) == 0 {
		return nil, nil
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"sku", "quantity"}); err != nil {
		return nil, err
	}
	for _, row := range pending {
		if err := w.Write([]string{row.sku, strconv.Itoa(row.qty)}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("metaldogs:catalog-seed: stock csv: %w", err)
	}
	return buf.Bytes(), nil
}

type productRow struct {
	line        int
	name        string
	slug        string
	parentSKU   string
	sku         string
	productType string
	status      string
	variantName string
	size        string
}

type priceRow struct {
	line     int
	sku      string
	currency string
	minor    int64
}

type stockRow struct {
	line int
	sku  string
	qty  int
}

func loadDir(dir string) ([]productRow, []priceRow, []stockRow, error) {
	products, err := loadProducts(filepath.Join(dir, productsFile))
	if err != nil {
		return nil, nil, nil, err
	}
	prices, err := loadPrices(filepath.Join(dir, pricesFile))
	if err != nil {
		return nil, nil, nil, err
	}
	stocks, err := loadStock(filepath.Join(dir, stockFile))
	if err != nil {
		return nil, nil, nil, err
	}
	if err := matchSKUs(products, prices, stocks); err != nil {
		return nil, nil, nil, err
	}
	return products, prices, stocks, nil
}

func matchSKUs(products []productRow, prices []priceRow, stocks []stockRow) error {
	want := map[string]struct{}{}
	for _, row := range products {
		want[row.sku] = struct{}{}
	}
	priced := map[string]struct{}{}
	for _, row := range prices {
		if _, ok := want[row.sku]; !ok {
			return fieldErr(pricesFile, row.line, "sku", fmt.Sprintf("unknown sku %q", row.sku))
		}
		if _, dup := priced[row.sku]; dup {
			return fieldErr(pricesFile, row.line, "sku", fmt.Sprintf("duplicate sku %q", row.sku))
		}
		priced[row.sku] = struct{}{}
	}
	stocked := map[string]struct{}{}
	for _, row := range stocks {
		if _, ok := want[row.sku]; !ok {
			return fieldErr(stockFile, row.line, "sku", fmt.Sprintf("unknown sku %q", row.sku))
		}
		if _, dup := stocked[row.sku]; dup {
			return fieldErr(stockFile, row.line, "sku", fmt.Sprintf("duplicate sku %q", row.sku))
		}
		stocked[row.sku] = struct{}{}
	}
	for sku := range want {
		if _, ok := priced[sku]; !ok {
			return fmt.Errorf("metaldogs:catalog-seed: %s: missing price for sku %q", pricesFile, sku)
		}
		if _, ok := stocked[sku]; !ok {
			return fmt.Errorf("metaldogs:catalog-seed: %s: missing stock for sku %q", stockFile, sku)
		}
	}
	return nil
}

func loadProducts(path string) ([]productRow, error) {
	records, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	col, err := headerIndex(productsFile, records[0], "name", "slug", "parent_sku", "sku", "type", "status", "variant_name", "size")
	if err != nil {
		return nil, err
	}
	var rows []productRow
	seenSKU := map[string]struct{}{}
	type identity struct {
		name, parent, productType, status string
	}
	idents := map[string]identity{}
	for i, rec := range records[1:] {
		line := i + 2
		row := productRow{
			line:        line,
			name:        cell(rec, col, "name"),
			slug:        cell(rec, col, "slug"),
			parentSKU:   cell(rec, col, "parent_sku"),
			sku:         cell(rec, col, "sku"),
			productType: cell(rec, col, "type"),
			status:      cell(rec, col, "status"),
			variantName: cell(rec, col, "variant_name"),
			size:        cell(rec, col, "size"),
		}
		for _, pair := range []struct{ col, val string }{
			{"name", row.name},
			{"slug", row.slug},
			{"parent_sku", row.parentSKU},
			{"sku", row.sku},
			{"type", row.productType},
			{"status", row.status},
			{"size", row.size},
		} {
			if pair.val == "" {
				return nil, fieldErr(productsFile, line, pair.col, "empty")
			}
		}
		if row.productType != string(domcat.TypeConfigurable) {
			return nil, fieldErr(productsFile, line, "type", fmt.Sprintf("want %s", domcat.TypeConfigurable))
		}
		if row.status != string(domcat.StatusDraft) {
			return nil, fieldErr(productsFile, line, "status", "want draft")
		}
		if _, dup := seenSKU[row.sku]; dup {
			return nil, fieldErr(productsFile, line, "sku", fmt.Sprintf("duplicate sku %q", row.sku))
		}
		seenSKU[row.sku] = struct{}{}
		id := identity{name: row.name, parent: row.parentSKU, productType: row.productType, status: row.status}
		if prev, ok := idents[row.slug]; ok && prev != id {
			return nil, fieldErr(productsFile, line, "slug", fmt.Sprintf("conflicting rows for slug %q", row.slug))
		}
		idents[row.slug] = id
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("metaldogs:catalog-seed: %s: no data rows", productsFile)
	}
	return rows, nil
}

func loadPrices(path string) ([]priceRow, error) {
	records, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	col, err := headerIndex(pricesFile, records[0], "sku", "currency", "amount")
	if err != nil {
		return nil, err
	}
	var rows []priceRow
	for i, rec := range records[1:] {
		line := i + 2
		sku := cell(rec, col, "sku")
		if sku == "" {
			return nil, fieldErr(pricesFile, line, "sku", "empty")
		}
		currency := strings.ToUpper(cell(rec, col, "currency"))
		if !shared.IsValidCurrency(currency) {
			return nil, fieldErr(pricesFile, line, "currency", fmt.Sprintf("invalid currency %q", cell(rec, col, "currency")))
		}
		minor, err := parseNetMinor(cellRaw(rec, col, "amount"))
		if err != nil {
			return nil, fieldErr(pricesFile, line, "amount", err.Error())
		}
		rows = append(rows, priceRow{line: line, sku: sku, currency: currency, minor: minor})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("metaldogs:catalog-seed: %s: no data rows", pricesFile)
	}
	return rows, nil
}

func loadStock(path string) ([]stockRow, error) {
	records, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	col, err := headerIndex(stockFile, records[0], "sku", "quantity")
	if err != nil {
		return nil, err
	}
	var rows []stockRow
	for i, rec := range records[1:] {
		line := i + 2
		sku := cell(rec, col, "sku")
		if sku == "" {
			return nil, fieldErr(stockFile, line, "sku", "empty")
		}
		qtyText := cell(rec, col, "quantity")
		qty, err := strconv.Atoi(qtyText)
		if err != nil || qty < 0 {
			return nil, fieldErr(stockFile, line, "quantity", fmt.Sprintf("invalid quantity %q", qtyText))
		}
		rows = append(rows, stockRow{line: line, sku: sku, qty: qty})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("metaldogs:catalog-seed: %s: no data rows", stockFile)
	}
	return rows, nil
}

func readCSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("metaldogs:catalog-seed: open %s: %w", filepath.Base(path), err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	var records [][]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("metaldogs:catalog-seed: %s: %w", filepath.Base(path), err)
		}
		records = append(records, rec)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("metaldogs:catalog-seed: %s: empty", filepath.Base(path))
	}
	return records, nil
}

func headerIndex(file string, header []string, required ...string) (map[string]int, error) {
	col := map[string]int{}
	for i, h := range header {
		name := strings.TrimSpace(strings.ToLower(strings.TrimPrefix(h, "\uFEFF")))
		col[name] = i
	}
	for _, name := range required {
		if _, ok := col[name]; !ok {
			return nil, fmt.Errorf("metaldogs:catalog-seed: %s: missing column %q", file, name)
		}
	}
	return col, nil
}

func cell(rec []string, col map[string]int, name string) string {
	return strings.TrimSpace(cellRaw(rec, col, name))
}

func cellRaw(rec []string, col map[string]int, name string) string {
	i, ok := col[name]
	if !ok || i >= len(rec) {
		return ""
	}
	return rec[i]
}

func fieldErr(file string, row int, column, msg string) error {
	return fmt.Errorf("metaldogs:catalog-seed: %s: row %d: column %s: %s", file, row, column, msg)
}
