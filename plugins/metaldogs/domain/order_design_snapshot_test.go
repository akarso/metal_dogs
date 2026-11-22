package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

func TestNewOrderDesignSnapshot_CopiesTextsAndRejectsPartialProduction(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 22, 18, 0, 0, 0, time.UTC)
	texts := map[string]string{"dog_name": "Burek"}
	s, err := domain.NewOrderDesignSnapshot(
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
		"44444444-4444-4444-8444-444444444444",
		"TEE-BLK-M",
		"black-tee",
		2,
		texts,
		"fake", "fake-model", "pv1",
		"55555555-5555-4555-8555-555555555555",
		"sha256:art",
		"", "",
		at,
	)
	if err != nil {
		t.Fatal(err)
	}
	texts["dog_name"] = "Azor"
	if s.Texts["dog_name"] != "Burek" {
		t.Fatalf("texts aliased input: %+v", s.Texts)
	}
	if _, err := domain.NewOrderDesignSnapshot(
		s.OrderID, s.VariantID, s.DesignID, s.DesignVersionID, s.SKU,
		s.TemplateSlug, s.TemplateVersion, s.Texts,
		s.Provider, s.Model, s.PromptVersion,
		s.ArtworkAssetID, s.ArtworkChecksum,
		"66666666-6666-4666-8666-666666666666", "",
		at,
	); err == nil {
		t.Fatal("partial production must fail")
	}
}

func TestOrderDesignSnapshot_FillProductionOnce(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 22, 18, 0, 0, 0, time.UTC)
	s, err := domain.NewOrderDesignSnapshot(
		"11111111-1111-4111-8111-111111111111",
		"v1",
		"33333333-3333-4333-8333-333333333333",
		"44444444-4444-4444-8444-444444444444",
		"TEE-BLK-M",
		"black-tee",
		1,
		map[string]string{"line": "x"},
		"fake", "fake-model", "pv1",
		"55555555-5555-4555-8555-555555555555",
		"sha256:art",
		"", "",
		at,
	)
	if err != nil {
		t.Fatal(err)
	}
	filled, err := s.FillProduction("66666666-6666-4666-8666-666666666666", "sha256:prod")
	if err != nil {
		t.Fatal(err)
	}
	if s.ProductionAssetID != "" || s.ProductionChecksum != "" {
		t.Fatal("FillProduction mutated the original")
	}
	if filled.ProductionAssetID != "66666666-6666-4666-8666-666666666666" || filled.ProductionChecksum != "sha256:prod" {
		t.Fatalf("%+v", filled)
	}
	if _, err := filled.FillProduction("77777777-7777-4777-8777-777777777777", "sha256:other"); !errors.Is(err, domain.ErrProductionAlreadySet) {
		t.Fatalf("second fill: %v", err)
	}
	spaced := s
	spaced.SKU = "  "
	if err := spaced.Validate(); err == nil {
		t.Fatal("whitespace sku must fail Validate")
	}
}
