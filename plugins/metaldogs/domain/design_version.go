package domain

import (
	"fmt"
	"strings"
	"time"
)

// DesignVersion production_status values (ADR-0006).
const (
	ProductionPending   = "pending"
	ProductionPreparing = "preparing"
	ProductionReady     = "ready"
	ProductionFailed    = "failed"
)

// ProductionStatuses is the golden list for docs/tests.
var ProductionStatuses = []string{
	ProductionPending,
	ProductionPreparing,
	ProductionReady,
	ProductionFailed,
}

// DesignVersion is an immutable freeze of an approved design (ADR-0005).
// Construct via NewDesignVersion; do not mutate fields after creation.
type DesignVersion struct {
	ID                 string
	DesignID           string
	TemplateID         string
	TemplateVersion    int
	Texts              map[string]string
	ArtworkAssetID     string
	ArtworkChecksum    string
	ProductionAssetID  string
	ProductionChecksum string
	ProductionStatus   string
	Provider           string
	Model              string
	PromptVersion      string
	ParametersJSON     []byte
	ApprovedAt         time.Time
}

// NewDesignVersion builds a pending production DesignVersion at approval time.
func NewDesignVersion(
	id, designID, templateID string,
	templateVersion int,
	texts map[string]string,
	artworkAssetID, artworkChecksum string,
	provider, model, promptVersion string,
	parametersJSON []byte,
	at time.Time,
) (DesignVersion, error) {
	id = strings.TrimSpace(id)
	designID = strings.TrimSpace(designID)
	templateID = strings.TrimSpace(templateID)
	artworkAssetID = strings.TrimSpace(artworkAssetID)
	artworkChecksum = strings.TrimSpace(artworkChecksum)
	if id == "" || designID == "" || templateID == "" {
		return DesignVersion{}, fmt.Errorf("design version: id, design_id, template_id required")
	}
	if templateVersion < 1 {
		return DesignVersion{}, fmt.Errorf("design version: template_version must be >= 1")
	}
	if artworkAssetID == "" || artworkChecksum == "" {
		return DesignVersion{}, fmt.Errorf("design version: artwork asset and checksum required")
	}
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	promptVersion = strings.TrimSpace(promptVersion)
	if provider == "" || model == "" || promptVersion == "" {
		return DesignVersion{}, fmt.Errorf("design version: provider, model, and prompt_version required")
	}
	copied := make(map[string]string, len(texts))
	for k, v := range texts {
		copied[k] = v
	}
	params := append([]byte(nil), parametersJSON...)
	if len(params) == 0 {
		params = []byte("{}")
	}
	return DesignVersion{
		ID:               id,
		DesignID:         designID,
		TemplateID:       templateID,
		TemplateVersion:  templateVersion,
		Texts:            copied,
		ArtworkAssetID:   artworkAssetID,
		ArtworkChecksum:  artworkChecksum,
		ProductionStatus: ProductionPending,
		Provider:         provider,
		Model:            model,
		PromptVersion:    promptVersion,
		ParametersJSON:   params,
		ApprovedAt:       at.UTC(),
	}, nil
}
