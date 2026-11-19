package catalog

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

const (
	productsFile = "products.csv"
	pricesFile   = "prices.csv"
	stockFile    = "stock.csv"

	parentSKUAttribute = "parent_sku"

	// Catalog seed port takes strings. Same literals as domain/catalog StatusDraft and TypeConfigurable.
	productStatusDraft      = "draft"
	productTypeConfigurable = "configurable"
)

// Seed loads the catalog CSVs and applies them with one CatalogSeedPort.Import.
// Files are validated before that call. Product Status is left empty: create
// becomes draft, and an existing active product is not a status mismatch.
func Seed(ctx context.Context, seed pluginsdk.CatalogSeedPort, dir string) error {
	if seed == nil {
		return fmt.Errorf("metaldogs:catalog-seed: catalog seed port is not configured")
	}
	batch, err := loadBatch(dir)
	if err != nil {
		return err
	}
	if _, err := seed.Import(ctx, batch); err != nil {
		return fmt.Errorf("metaldogs:catalog-seed: %w", err)
	}
	return nil
}

func loadBatch(dir string) (pluginsdk.CatalogSeedBatch, error) {
	products, prices, stocks, err := loadDir(dir)
	if err != nil {
		return pluginsdk.CatalogSeedBatch{}, err
	}
	var batch pluginsdk.CatalogSeedBatch
	seenSlug := map[string]struct{}{}
	for _, row := range products {
		if _, ok := seenSlug[row.slug]; !ok {
			seenSlug[row.slug] = struct{}{}
			batch.Products = append(batch.Products, pluginsdk.CatalogProductSeed{
				Name: row.name,
				Slug: row.slug,
				Type: row.productType,
				Attributes: map[string]any{
					parentSKUAttribute: row.parentSKU,
				},
			})
		}
		batch.Variants = append(batch.Variants, pluginsdk.CatalogVariantSeed{
			ProductSlug: row.slug,
			SKU:         row.sku,
			Name:        row.variantName,
			Attributes:  map[string]any{"size": row.size},
		})
	}
	for _, row := range prices {
		batch.Prices = append(batch.Prices, pluginsdk.CatalogPriceSeed{
			VariantSKU: row.sku,
			Amount:     row.minor,
			Currency:   row.currency,
			Update:     true,
		})
	}
	for _, row := range stocks {
		batch.Stock = append(batch.Stock, pluginsdk.CatalogStockSeed{
			VariantSKU: row.sku,
			Qty:        row.qty,
		})
	}
	return batch, nil
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
		if row.productType != productTypeConfigurable {
			return nil, fieldErr(productsFile, line, "type", fmt.Sprintf("want %s", productTypeConfigurable))
		}
		if row.status != productStatusDraft {
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
