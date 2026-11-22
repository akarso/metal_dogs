package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrProductionAlreadySet is returned when production artwork is set a second time.
var ErrProductionAlreadySet = errors.New("order design snapshot production already set")

// OrderDesignSnapshot is the immutable copy of an approved design on one order line
// (ADR-0005). Construct via NewOrderDesignSnapshot. Production artwork is filled once
// via FillProduction; there are no setters.
type OrderDesignSnapshot struct {
	OrderID            string
	VariantID          string
	DesignID           string
	DesignVersionID    string
	SKU                string
	TemplateSlug       string
	TemplateVersion    int
	Texts              map[string]string
	Provider           string
	Model              string
	PromptVersion      string
	ArtworkAssetID     string
	ArtworkChecksum    string
	ProductionAssetID  string
	ProductionChecksum string
	CreatedAt          time.Time
}

// NewOrderDesignSnapshot copies line and approved-version data.
// Production asset id and checksum are both empty or both set.
func NewOrderDesignSnapshot(
	orderID, variantID, designID, designVersionID, sku string,
	templateSlug string,
	templateVersion int,
	texts map[string]string,
	provider, model, promptVersion string,
	artworkAssetID, artworkChecksum string,
	productionAssetID, productionChecksum string,
	createdAt time.Time,
) (OrderDesignSnapshot, error) {
	orderID = strings.TrimSpace(orderID)
	variantID = strings.TrimSpace(variantID)
	designID = strings.TrimSpace(designID)
	designVersionID = strings.TrimSpace(designVersionID)
	sku = strings.TrimSpace(sku)
	templateSlug = strings.TrimSpace(templateSlug)
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	promptVersion = strings.TrimSpace(promptVersion)
	artworkAssetID = strings.TrimSpace(artworkAssetID)
	artworkChecksum = strings.TrimSpace(artworkChecksum)
	productionAssetID = strings.TrimSpace(productionAssetID)
	productionChecksum = strings.TrimSpace(productionChecksum)
	if orderID == "" || variantID == "" || designID == "" || designVersionID == "" || sku == "" {
		return OrderDesignSnapshot{}, fmt.Errorf("order design snapshot: order, variant, design, version, and sku required")
	}
	if templateSlug == "" || templateVersion < 1 {
		return OrderDesignSnapshot{}, fmt.Errorf("order design snapshot: template slug and version required")
	}
	if provider == "" || model == "" || promptVersion == "" {
		return OrderDesignSnapshot{}, fmt.Errorf("order design snapshot: provider, model, and prompt_version required")
	}
	if artworkAssetID == "" || artworkChecksum == "" {
		return OrderDesignSnapshot{}, fmt.Errorf("order design snapshot: artwork asset and checksum required")
	}
	if (productionAssetID == "") != (productionChecksum == "") {
		return OrderDesignSnapshot{}, fmt.Errorf("order design snapshot: production asset and checksum are set together")
	}
	if createdAt.IsZero() {
		return OrderDesignSnapshot{}, fmt.Errorf("order design snapshot: created_at required")
	}
	return OrderDesignSnapshot{
		OrderID:            orderID,
		VariantID:          variantID,
		DesignID:           designID,
		DesignVersionID:    designVersionID,
		SKU:                sku,
		TemplateSlug:       templateSlug,
		TemplateVersion:    templateVersion,
		Texts:              copyTexts(texts),
		Provider:           provider,
		Model:              model,
		PromptVersion:      promptVersion,
		ArtworkAssetID:     artworkAssetID,
		ArtworkChecksum:    artworkChecksum,
		ProductionAssetID:  productionAssetID,
		ProductionChecksum: productionChecksum,
		CreatedAt:          createdAt.UTC(),
	}, nil
}

// FillProduction returns a copy with production artwork set.
// A second call returns ErrProductionAlreadySet.
func (s OrderDesignSnapshot) FillProduction(assetID, checksum string) (OrderDesignSnapshot, error) {
	if s.ProductionAssetID != "" || s.ProductionChecksum != "" {
		return OrderDesignSnapshot{}, ErrProductionAlreadySet
	}
	assetID = strings.TrimSpace(assetID)
	checksum = strings.TrimSpace(checksum)
	if assetID == "" || checksum == "" {
		return OrderDesignSnapshot{}, fmt.Errorf("order design snapshot: production asset and checksum required")
	}
	next := s
	next.Texts = copyTexts(s.Texts)
	next.ProductionAssetID = assetID
	next.ProductionChecksum = checksum
	return next, nil
}

func copyTexts(texts map[string]string) map[string]string {
	copied := make(map[string]string, len(texts))
	for k, v := range texts {
		copied[k] = v
	}
	return copied
}
