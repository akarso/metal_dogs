package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestOrderDesignSnapshotRepo_InsertRoundTripFillAndRules(t *testing.T) {
	db := testutil.OpenIsolated(t, "order_design_snapshots")
	repo := postgres.NewOrderDesignSnapshotRepo(db)
	ctx := context.Background()
	at := time.Date(2026, 11, 22, 18, 4, 5, 0, time.UTC)

	orderID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	otherOrder := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	variantM := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	variantL := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	designID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	versionID := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	artID := "11111111-1111-4111-8111-111111111111"
	prodID := "22222222-2222-4222-8222-222222222222"

	lineM, err := domain.NewOrderDesignSnapshot(
		orderID, variantM, designID, versionID, "TEE-BLK-M",
		"black-tee", 3,
		map[string]string{"dog_name": "Burek", "line_2": "metal"},
		"openai", "gpt-image", "prompt-v7",
		artID, "sha256:artwork",
		"", "",
		at,
	)
	if err != nil {
		t.Fatal(err)
	}
	written, err := repo.Insert(ctx, lineM)
	if err != nil || !written {
		t.Fatalf("insert written=%v err=%v", written, err)
	}
	spaced := lineM
	spaced.VariantID = "99999999-9999-4999-8999-999999999990"
	spaced.SKU = "  "
	written, err = repo.Insert(ctx, spaced)
	if err == nil || written {
		t.Fatalf("whitespace sku written=%v err=%v", written, err)
	}
	clash := lineM
	clash.SKU = "TEE-BLK-M-TAMPERED"
	clash.ArtworkChecksum = "sha256:other"
	written, err = repo.Insert(ctx, clash)
	if err != nil || written {
		t.Fatalf("second insert written=%v err=%v", written, err)
	}

	got, ok, err := repo.Get(ctx, orderID, variantM)
	if err != nil || !ok {
		t.Fatalf("get ok=%v err=%v", ok, err)
	}
	if got.SKU != "TEE-BLK-M" || got.ArtworkChecksum != "sha256:artwork" {
		t.Fatalf("conflict overwrote row: %+v", got)
	}
	assertSnapshotEqual(t, lineM, got)

	lineL := lineM
	lineL.VariantID = variantL
	lineL.SKU = "TEE-BLK-L"
	written, err = repo.Insert(ctx, lineL)
	if err != nil || !written {
		t.Fatalf("second line written=%v err=%v", written, err)
	}
	other, err := domain.NewOrderDesignSnapshot(
		otherOrder, variantM, designID, versionID, "TEE-BLK-M",
		"black-tee", 3, lineM.Texts,
		lineM.Provider, lineM.Model, lineM.PromptVersion,
		artID, "sha256:artwork", "", "", at,
	)
	if err != nil {
		t.Fatal(err)
	}
	if written, err = repo.Insert(ctx, other); err != nil || !written {
		t.Fatalf("other order written=%v err=%v", written, err)
	}
	listed, err := repo.ListByOrder(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].VariantID != variantM || listed[1].VariantID != variantL {
		t.Fatalf("list: %+v", listed)
	}

	if err := postgres.NewAssetRepo(db).Insert(ctx, domain.Asset{
		ID: prodID, Type: domain.AssetTypeProduction,
		StorageKey: "private/blobs/metaldogs/production/" + prodID + ".png",
		MimeType:   "image/png", SizeBytes: 1, Width: 8, Height: 8,
		Checksum: "sha256:production", CreatedAt: at, DeleteAfter: at.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.FillProduction(ctx, orderID, variantM, prodID, "sha256:production"); err != nil {
		t.Fatal(err)
	}
	filled, ok, err := repo.Get(ctx, orderID, variantM)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if filled.ProductionAssetID != prodID || filled.ProductionChecksum != "sha256:production" {
		t.Fatalf("fill: %+v", filled)
	}
	if filled.SKU != lineM.SKU || filled.ArtworkChecksum != lineM.ArtworkChecksum {
		t.Fatalf("fill changed frozen fields: %+v", filled)
	}
	if err := repo.FillProduction(ctx, orderID, variantM, "33333333-3333-4333-8333-333333333333", "sha256:second"); !errors.Is(err, domain.ErrProductionAlreadySet) {
		t.Fatalf("second fill: %v", err)
	}
	again, _, err := repo.Get(ctx, orderID, variantM)
	if err != nil {
		t.Fatal(err)
	}
	if again.ProductionChecksum != "sha256:production" {
		t.Fatalf("second fill wrote %q", again.ProductionChecksum)
	}
	if err := repo.FillProduction(ctx, otherOrder, "missing-variant", prodID, "sha256:production"); !errors.Is(err, ports.ErrSnapshotNotFound) {
		t.Fatalf("missing line: %v", err)
	}

	frozen := []struct {
		column string
		value  any
		cast   string
	}{
		{"order_id", "99999999-9999-4999-8999-999999999999", ""},
		{"variant_id", "tampered-variant", ""},
		{"design_id", "99999999-9999-4999-8999-999999999991", ""},
		{"design_version_id", "99999999-9999-4999-8999-999999999992", ""},
		{"sku", "nope", ""},
		{"template_slug", "tampered", ""},
		{"template_version", 99, ""},
		{"texts", `{"dog_name":"Azor"}`, "::jsonb"},
		{"provider", "tampered", ""},
		{"model", "tampered-model", ""},
		{"prompt_version", "tampered-prompt", ""},
		{"artwork_asset_id", "99999999-9999-4999-8999-999999999993", ""},
		{"artwork_checksum", "sha256:tampered", ""},
		{"created_at", at.Add(time.Hour), ""},
	}
	for _, u := range frozen {
		q := fmt.Sprintf(
			`UPDATE metaldogs_order_design_snapshots SET %s = $3%s WHERE order_id = $1 AND variant_id = $2`,
			u.column, u.cast,
		)
		_, err = db.ExecContext(ctx, q, orderID, variantM, u.value)
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("%s update: %v", u.column, err)
		}
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE metaldogs_order_design_snapshots
		SET production_checksum = 'sha256:tampered'
		WHERE order_id = $1 AND variant_id = $2
	`, orderID, variantM); err == nil {
		t.Fatal("second production update must fail")
	}
	if _, err := db.ExecContext(ctx, `
		DELETE FROM metaldogs_order_design_snapshots WHERE order_id = $1 AND variant_id = $2
	`, orderID, variantL); err == nil {
		t.Fatal("delete must fail")
	}
	var n int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM metaldogs_order_design_snapshots WHERE order_id = $1
	`, orderID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("rows after refused delete: %d", n)
	}
	unchanged, ok, err := repo.Get(ctx, orderID, variantM)
	if err != nil || !ok {
		t.Fatalf("row after refused updates ok=%v err=%v", ok, err)
	}
	if unchanged.SKU != lineM.SKU || unchanged.ArtworkChecksum != lineM.ArtworkChecksum ||
		unchanged.ProductionChecksum != "sha256:production" || !unchanged.CreatedAt.Equal(at) {
		t.Fatalf("refused updates changed the row: %+v", unchanged)
	}

	if _, ok, err := repo.Get(ctx, "not-a-uuid", variantM); !errors.Is(err, ports.ErrInvalidOrderID) || ok {
		t.Fatalf("get bad order id ok=%v err=%v", ok, err)
	}
	if _, err := repo.ListByOrder(ctx, "not-a-uuid"); !errors.Is(err, ports.ErrInvalidOrderID) {
		t.Fatalf("list bad order id: %v", err)
	}
	if err := repo.FillProduction(ctx, "not-a-uuid", variantM, prodID, "sha256:production"); !errors.Is(err, ports.ErrInvalidOrderID) {
		t.Fatalf("fill bad order id: %v", err)
	}
	upper, ok, err := repo.Get(ctx, strings.ToUpper(orderID), variantM)
	if err != nil || !ok || upper.SKU != lineM.SKU {
		t.Fatalf("uppercase order id ok=%v err=%v sku=%q", ok, err, upper.SKU)
	}

	var prev014, snap015 time.Time
	if err := db.QueryRowContext(ctx, `
		SELECT applied_at FROM schema_migrations WHERE version = $1
	`, "plugin:metaldogs:014_design_versions_append_only.sql").Scan(&prev014); err != nil {
		t.Fatalf("014 not applied: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT applied_at FROM schema_migrations WHERE version = $1
	`, "plugin:metaldogs:015_create_order_design_snapshots.sql").Scan(&snap015); err != nil {
		t.Fatalf("015 not applied: %v", err)
	}
	if snap015.Before(prev014) {
		t.Fatalf("015 applied_at %s before 014 %s", snap015, prev014)
	}
	var indexes int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND indexname IN (
			'metaldogs_order_design_snapshots_artwork_asset_id_idx',
			'metaldogs_order_design_snapshots_production_asset_id_idx'
		  )
	`).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != 2 {
		t.Fatalf("snapshot asset indexes=%d", indexes)
	}
}

func TestFillProduction_SoftDeletedAssetFails(t *testing.T) {
	db := testutil.OpenIsolated(t, "order_design_snapshots_dead_asset")
	repo := postgres.NewOrderDesignSnapshotRepo(db)
	ctx := context.Background()
	at := time.Date(2026, 11, 22, 18, 4, 5, 0, time.UTC)
	orderID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaab"
	variantID := "cccccccc-cccc-4ccc-8ccc-cccccccccccb"
	artID := "11111111-1111-4111-8111-111111111112"
	prodID := "22222222-2222-4222-8222-222222222223"
	line, err := domain.NewOrderDesignSnapshot(
		orderID, variantID, "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeef", "ffffffff-ffff-4fff-8fff-fffffffffffe", "TEE-BLK-M",
		"black-tee", 3, map[string]string{"dog_name": "Burek"},
		"openai", "gpt-image", "prompt-v7",
		artID, "sha256:artwork", "", "", at,
	)
	if err != nil {
		t.Fatal(err)
	}
	if written, err := repo.Insert(ctx, line); err != nil || !written {
		t.Fatalf("written=%v err=%v", written, err)
	}
	if err := postgres.NewAssetRepo(db).Insert(ctx, domain.Asset{
		ID: prodID, Type: domain.AssetTypeProduction,
		StorageKey: "private/blobs/metaldogs/production/" + prodID + ".png",
		MimeType:   "image/png", SizeBytes: 1, Width: 8, Height: 8,
		Checksum: "sha256:production", CreatedAt: at, DeleteAfter: at.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE metaldogs_assets SET deleted_at = $2 WHERE id = $1`, prodID, at); err != nil {
		t.Fatal(err)
	}
	err = repo.FillProduction(ctx, orderID, variantID, prodID, "sha256:production")
	if err == nil || !strings.Contains(err.Error(), "missing or deleted") {
		t.Fatalf("err=%v", err)
	}
	got, ok, err := repo.Get(ctx, orderID, variantID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if got.ProductionAssetID != "" || got.ProductionChecksum != "" {
		t.Fatalf("fill wrote deleted asset: %+v", got)
	}
}

func assertSnapshotEqual(t *testing.T, want, got domain.OrderDesignSnapshot) {
	t.Helper()
	if want.OrderID != got.OrderID || want.VariantID != got.VariantID ||
		want.DesignID != got.DesignID || want.DesignVersionID != got.DesignVersionID ||
		want.SKU != got.SKU || want.TemplateSlug != got.TemplateSlug ||
		want.TemplateVersion != got.TemplateVersion ||
		want.Provider != got.Provider || want.Model != got.Model ||
		want.PromptVersion != got.PromptVersion ||
		want.ArtworkAssetID != got.ArtworkAssetID || want.ArtworkChecksum != got.ArtworkChecksum ||
		want.ProductionAssetID != got.ProductionAssetID ||
		want.ProductionChecksum != got.ProductionChecksum ||
		!want.CreatedAt.Equal(got.CreatedAt) {
		t.Fatalf("round trip\nwant %+v\ngot  %+v", want, got)
	}
	if len(want.Texts) != len(got.Texts) {
		t.Fatalf("texts %+v vs %+v", want.Texts, got.Texts)
	}
	for k, v := range want.Texts {
		if got.Texts[k] != v {
			t.Fatalf("texts[%s]=%q want %q", k, got.Texts[k], v)
		}
	}
}
