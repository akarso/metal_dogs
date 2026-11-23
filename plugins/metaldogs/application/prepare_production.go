package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/production"
)

// productionAssetNamespace is the UUID v5 namespace for a version's production asset id.
var productionAssetNamespace = uuid.MustParse("05a00000-0000-4000-8000-0000000005a0")

// PreparePermanentError is a validation failure already stored on the version.
// The queue must not treat it as a transient retry.
type PreparePermanentError struct {
	Code string
}

func (e *PreparePermanentError) Error() string {
	if e == nil || e.Code == "" {
		return "metaldogs prepare: failed"
	}
	return "metaldogs prepare: " + e.Code
}

// PrepareRetryError is a storage or database failure. Op is "storage" or "database".
// Error text never includes object keys or image bytes.
type PrepareRetryError struct {
	Op string
}

func (e *PrepareRetryError) Error() string {
	if e == nil || e.Op == "" {
		return "metaldogs prepare: database"
	}
	return "metaldogs prepare: " + e.Op
}

// ProductionAssetID is the UUID v5 of the design version id.
// A second run addresses the same asset row and object.
func ProductionAssetID(versionID string) (string, error) {
	versionID = strings.TrimSpace(versionID)
	parsed, err := uuid.Parse(versionID)
	if err != nil {
		return "", fmt.Errorf("metaldogs prepare: design_version_id: %w", err)
	}
	return uuid.NewSHA1(productionAssetNamespace, []byte(parsed.String())).String(), nil
}

// PrepareProduction fits the approved artwork and records one production asset.
type PrepareProduction struct {
	Versions ports.DesignVersionRepository
	Assets   ports.AssetRepository
	Designs  ports.DesignRepository
	Store    ports.AssetStore
	Spec     *production.Spec
	Clock    ports.Clock
}

// Execute prepares versionID. wroteReady is true only when this call stored the file.
// An already ready version returns false, nil and does not write.
func (s *PrepareProduction) Execute(ctx context.Context, versionID, designID string) (bool, error) {
	if s == nil || s.Versions == nil || s.Assets == nil || s.Designs == nil || s.Store == nil || s.Spec == nil {
		return false, &PrepareRetryError{Op: "database"}
	}
	versionID = strings.TrimSpace(versionID)
	designID = strings.TrimSpace(designID)
	if versionID == "" || designID == "" {
		return false, fmt.Errorf("metaldogs prepare: design_version_id and design_id required")
	}
	v, ok, err := s.Versions.GetByID(ctx, versionID)
	if err != nil {
		return false, &PrepareRetryError{Op: "database"}
	}
	if !ok {
		return false, &PreparePermanentError{Code: "version_not_found"}
	}
	if v.DesignID != designID {
		return false, &PreparePermanentError{Code: "design_mismatch"}
	}
	switch v.ProductionStatus {
	case domain.ProductionReady:
		return false, nil
	case domain.ProductionFailed:
		code := v.ProductionFailureReason
		if code == "" {
			code = domain.ProductionFailed
		}
		return false, &PreparePermanentError{Code: code}
	case domain.ProductionPending, domain.ProductionPreparing:
	default:
		return false, &PreparePermanentError{Code: domain.ProductionFailed}
	}
	if err := s.Versions.MarkProductionPreparing(ctx, versionID); err != nil {
		if errors.Is(err, ports.ErrProductionAlreadyReady) {
			return false, nil
		}
		if errors.Is(err, ports.ErrProductionNotOpen) {
			return s.alreadyClosed(ctx, versionID)
		}
		return false, &PrepareRetryError{Op: "database"}
	}
	art, aok, err := s.Assets.GetByID(ctx, v.ArtworkAssetID)
	if err != nil {
		return false, &PrepareRetryError{Op: "database"}
	}
	if !aok || art.StorageKey == "" {
		return s.fail(ctx, versionID, production.CodeArtworkUnreadable)
	}
	raw, err := s.Store.Read(ctx, art.StorageKey)
	if err != nil {
		return false, &PrepareRetryError{Op: "storage"}
	}
	out, err := production.Prepare(raw, *s.Spec)
	if err != nil {
		var pe *production.Error
		if errors.As(err, &pe) {
			return s.fail(ctx, versionID, pe.Code)
		}
		return false, &PrepareRetryError{Op: "database"}
	}
	assetID, err := ProductionAssetID(versionID)
	if err != nil {
		return false, &PreparePermanentError{Code: "version_not_found"}
	}
	now := s.now()
	key, err := ports.ObjectKey(domain.AssetTypeProduction, assetID, "png", now)
	if err != nil {
		return false, &PrepareRetryError{Op: "database"}
	}
	if err := s.Store.Put(ctx, key, bytes.NewReader(out.PNG), "image/png"); err != nil {
		return false, &PrepareRetryError{Op: "storage"}
	}
	deleteAfter, err := s.deleteAfter(ctx, designID, now)
	if err != nil {
		return false, err
	}
	asset := domain.Asset{
		ID:          assetID,
		Type:        domain.AssetTypeProduction,
		StorageKey:  key,
		MimeType:    "image/png",
		SizeBytes:   int64(len(out.PNG)),
		Width:       s.Spec.WidthPx,
		Height:      s.Spec.HeightPx,
		Checksum:    out.Checksum,
		OwnerRef:    designID,
		CreatedAt:   now,
		DeleteAfter: deleteAfter,
	}
	if err := s.Versions.CommitProductionReady(ctx, versionID, asset); err != nil {
		if errors.Is(err, ports.ErrProductionAlreadyReady) {
			return false, nil
		}
		return false, &PrepareRetryError{Op: "database"}
	}
	return true, nil
}

func (s *PrepareProduction) now() time.Time {
	if s.Clock == nil {
		return time.Now().UTC()
	}
	return s.Clock.Now().UTC()
}

func (s *PrepareProduction) deleteAfter(ctx context.Context, designID string, now time.Time) (time.Time, error) {
	d, ok, err := s.Designs.GetByID(ctx, designID)
	if err != nil || !ok {
		return time.Time{}, &PrepareRetryError{Op: "database"}
	}
	if d.ExpiresAt.After(now) {
		return d.ExpiresAt.UTC(), nil
	}
	return now.Add(domain.DesignRetentionUnordered), nil
}

func (s *PrepareProduction) fail(ctx context.Context, versionID, code string) (bool, error) {
	if err := s.Versions.MarkProductionFailed(ctx, versionID, code); err != nil {
		if errors.Is(err, ports.ErrProductionAlreadyReady) {
			return false, nil
		}
		if errors.Is(err, ports.ErrProductionNotOpen) {
			return s.alreadyClosed(ctx, versionID)
		}
		return false, &PrepareRetryError{Op: "database"}
	}
	return false, &PreparePermanentError{Code: code}
}

func (s *PrepareProduction) alreadyClosed(ctx context.Context, versionID string) (bool, error) {
	v, ok, err := s.Versions.GetByID(ctx, versionID)
	if err != nil || !ok {
		return false, &PrepareRetryError{Op: "database"}
	}
	if v.ProductionStatus == domain.ProductionReady {
		return false, nil
	}
	if v.ProductionStatus == domain.ProductionFailed {
		code := v.ProductionFailureReason
		if code == "" {
			code = domain.ProductionFailed
		}
		return false, &PreparePermanentError{Code: code}
	}
	return false, &PrepareRetryError{Op: "database"}
}
