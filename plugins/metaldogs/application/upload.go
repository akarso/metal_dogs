package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application/imageprep"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// UploadRetention is how long unattached source uploads remain before O2 sweep.
const UploadRetention = 2 * time.Hour

// UploadResult is the customer-facing create outcome (no storage keys).
type UploadResult struct {
	UploadID   string
	PreviewURL string
	Width      int
	Height     int
	MimeType   string
	ExpiresAt  time.Time
}

// UploadError is a classified upload failure for HTTP mapping.
type UploadError struct {
	Code    string
	Message string
}

func (e *UploadError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

// Stable error codes (HTTP layer maps status).
const (
	UploadErrPayloadTooLarge      = "payload_too_large"
	UploadErrUnsupportedMedia     = "unsupported_media_type"
	UploadErrInvalidImage         = "invalid_image"
	UploadErrDimensionsOutOfRange = "dimensions_out_of_range"
	UploadErrUnavailable          = "unavailable"
)

// UploadService creates source assets from raw photo bytes.
type UploadService struct {
	Assets ports.AssetRepository
	Store  ports.AssetStore
	IDs    ports.IDGenerator
	Clock  ports.Clock
}

// Create sniffs/decodes/re-encodes, Puts the blob, inserts metadata, and returns a signed preview URL.
// filenameHint is ignored for accept/reject (magic bytes only); reserved for future diagnostics.
func (s *UploadService) Create(ctx context.Context, raw []byte, _ string) (UploadResult, error) {
	if s == nil || s.Assets == nil || s.Store == nil || s.IDs == nil || s.Clock == nil {
		return UploadResult{}, &UploadError{Code: UploadErrUnavailable, Message: "upload deps missing"}
	}
	if len(raw) > generation.MaxPhotoBytes {
		return UploadResult{}, &UploadError{Code: UploadErrPayloadTooLarge, Message: "file too large"}
	}

	prep, err := imageprep.Prepare(raw)
	if err != nil {
		var pe *imageprep.Error
		if errors.As(err, &pe) {
			switch pe.Code {
			case imageprep.ErrUnsupportedMedia:
				return UploadResult{}, &UploadError{Code: UploadErrUnsupportedMedia, Message: pe.Message}
			case imageprep.ErrInvalidImage:
				return UploadResult{}, &UploadError{Code: UploadErrInvalidImage, Message: pe.Message}
			case imageprep.ErrDimensions:
				return UploadResult{}, &UploadError{Code: UploadErrDimensionsOutOfRange, Message: pe.Message}
			}
		}
		return UploadResult{}, fmt.Errorf("upload: prepare: %w", err)
	}

	now := s.Clock.Now().UTC()
	id := s.IDs.New()
	key, err := ports.ObjectKey(domain.AssetTypeSource, id, prep.Ext, now)
	if err != nil {
		return UploadResult{}, fmt.Errorf("upload: object key: %w", err)
	}
	sum := sha256.Sum256(prep.Bytes)
	asset := domain.Asset{
		ID:          id,
		Type:        domain.AssetTypeSource,
		StorageKey:  key,
		MimeType:    prep.MimeType,
		SizeBytes:   int64(len(prep.Bytes)),
		Width:       prep.Width,
		Height:      prep.Height,
		Checksum:    hex.EncodeToString(sum[:]),
		CreatedAt:   now,
		DeleteAfter: now.Add(UploadRetention),
	}

	if err := s.Store.Put(ctx, key, bytes.NewReader(prep.Bytes), prep.MimeType); err != nil {
		return UploadResult{}, fmt.Errorf("upload: put: %w", err)
	}
	if err := s.Assets.Insert(ctx, asset); err != nil {
		_ = s.Store.Delete(ctx, key) // best-effort orphan cleanup
		return UploadResult{}, fmt.Errorf("upload: insert: %w", err)
	}
	url, err := s.Store.SignedGetURL(ctx, key, ports.DefaultSignedGetTTL)
	if err != nil {
		return UploadResult{}, fmt.Errorf("upload: signed url: %w", err)
	}
	return UploadResult{
		UploadID:   id,
		PreviewURL: url,
		Width:      prep.Width,
		Height:     prep.Height,
		MimeType:   prep.MimeType,
		ExpiresAt:  asset.DeleteAfter,
	}, nil
}
