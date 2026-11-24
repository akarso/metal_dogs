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
	MockupAssetID      string
	ThumbnailAssetID   string
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
	s := OrderDesignSnapshot{
		OrderID:            strings.TrimSpace(orderID),
		VariantID:          strings.TrimSpace(variantID),
		DesignID:           strings.TrimSpace(designID),
		DesignVersionID:    strings.TrimSpace(designVersionID),
		SKU:                strings.TrimSpace(sku),
		TemplateSlug:       strings.TrimSpace(templateSlug),
		TemplateVersion:    templateVersion,
		Texts:              copyTexts(texts),
		Provider:           strings.TrimSpace(provider),
		Model:              strings.TrimSpace(model),
		PromptVersion:      strings.TrimSpace(promptVersion),
		ArtworkAssetID:     strings.TrimSpace(artworkAssetID),
		ArtworkChecksum:    strings.TrimSpace(artworkChecksum),
		ProductionAssetID:  strings.TrimSpace(productionAssetID),
		ProductionChecksum: strings.TrimSpace(productionChecksum),
		CreatedAt:          createdAt.UTC(),
	}
	if err := s.Validate(); err != nil {
		return OrderDesignSnapshot{}, err
	}
	return s, nil
}

// Validate checks the invariants NewOrderDesignSnapshot enforces after trimming.
func (s OrderDesignSnapshot) Validate() error {
	if strings.TrimSpace(s.OrderID) == "" || strings.TrimSpace(s.VariantID) == "" ||
		strings.TrimSpace(s.DesignID) == "" || strings.TrimSpace(s.DesignVersionID) == "" ||
		strings.TrimSpace(s.SKU) == "" {
		return fmt.Errorf("order design snapshot: order, variant, design, version, and sku required")
	}
	if strings.TrimSpace(s.TemplateSlug) == "" || s.TemplateVersion < 1 {
		return fmt.Errorf("order design snapshot: template slug and version required")
	}
	if strings.TrimSpace(s.Provider) == "" || strings.TrimSpace(s.Model) == "" || strings.TrimSpace(s.PromptVersion) == "" {
		return fmt.Errorf("order design snapshot: provider, model, and prompt_version required")
	}
	if strings.TrimSpace(s.ArtworkAssetID) == "" || strings.TrimSpace(s.ArtworkChecksum) == "" {
		return fmt.Errorf("order design snapshot: artwork asset and checksum required")
	}
	if (strings.TrimSpace(s.ProductionAssetID) == "") != (strings.TrimSpace(s.ProductionChecksum) == "") {
		return fmt.Errorf("order design snapshot: production asset and checksum are set together")
	}
	if (strings.TrimSpace(s.MockupAssetID) == "") != (strings.TrimSpace(s.ThumbnailAssetID) == "") {
		return fmt.Errorf("order design snapshot: mockup and thumbnail are set together")
	}
	if s.CreatedAt.IsZero() {
		return fmt.Errorf("order design snapshot: created_at required")
	}
	return nil
}

// WithThumbnail returns a copy that records the approved mockup and its thumbnail.
// Both ids are required. The original is unchanged.
func (s OrderDesignSnapshot) WithThumbnail(mockupAssetID, thumbnailAssetID string) (OrderDesignSnapshot, error) {
	mockupAssetID = strings.TrimSpace(mockupAssetID)
	thumbnailAssetID = strings.TrimSpace(thumbnailAssetID)
	if mockupAssetID == "" || thumbnailAssetID == "" {
		return OrderDesignSnapshot{}, fmt.Errorf("order design snapshot: mockup and thumbnail are set together")
	}
	next := s
	next.Texts = copyTexts(s.Texts)
	next.MockupAssetID = mockupAssetID
	next.ThumbnailAssetID = thumbnailAssetID
	if err := next.Validate(); err != nil {
		return OrderDesignSnapshot{}, err
	}
	return next, nil
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
