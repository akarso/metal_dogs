package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application/imageprep"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// UploadRetention is how long unattached source uploads remain before O2 sweep.
const UploadRetention = 2 * time.Hour

// DefaultMaxConcurrentUploads caps in-process upload prep/Put concurrency (pre-O4).
const DefaultMaxConcurrentUploads = 4

// ChecksumSHA256Prefix prefixes stored checksums for forward clarity.
const ChecksumSHA256Prefix = "sha256:"

// UploadResult is the customer-facing create outcome (no storage keys).
type UploadResult struct {
	UploadID            string
	PreviewURL          string
	PreviewURLExpiresAt time.Time
	DeleteAfter         time.Time
	Width               int
	Height              int
	MimeType            string
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
	UploadErrTooManyRequests      = "too_many_requests"
)

// UploadService creates source assets from raw photo bytes.
type UploadService struct {
	Assets ports.AssetRepository
	Store  ports.AssetStore
	IDs    ports.IDGenerator
	Clock  ports.Clock
	// MaxInFlight caps concurrent Create calls; 0 → DefaultMaxConcurrentUploads.
	MaxInFlight int

	once     sync.Once
	inFlight chan struct{}
}

func (s *UploadService) acquire() bool {
	s.once.Do(func() {
		n := s.MaxInFlight
		if n <= 0 {
			n = DefaultMaxConcurrentUploads
		}
		s.inFlight = make(chan struct{}, n)
	})
	select {
	case s.inFlight <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *UploadService) release() {
	select {
	case <-s.inFlight:
	default:
	}
}

// Create sniffs/decodes/re-encodes, Puts the blob, inserts metadata, and returns a signed preview URL.
func (s *UploadService) Create(ctx context.Context, raw []byte) (UploadResult, error) {
	if s == nil || s.Assets == nil || s.Store == nil || s.IDs == nil || s.Clock == nil {
		return UploadResult{}, &UploadError{Code: UploadErrUnavailable, Message: "upload deps missing"}
	}
	if !s.acquire() {
		return UploadResult{}, &UploadError{Code: UploadErrTooManyRequests, Message: "upload concurrency limit"}
	}
	defer s.release()

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
	previewTTL := ports.DefaultSignedGetTTL
	deleteAfter := now.Add(UploadRetention)
	asset := domain.Asset{
		ID:          id,
		Type:        domain.AssetTypeSource,
		StorageKey:  key,
		MimeType:    prep.MimeType,
		SizeBytes:   int64(len(prep.Bytes)),
		Width:       prep.Width,
		Height:      prep.Height,
		Checksum:    ChecksumSHA256Prefix + hex.EncodeToString(sum[:]),
		CreatedAt:   now,
		DeleteAfter: deleteAfter,
		OwnerRef:    "", // C3 fills capability/session binding
	}

	if err := s.Store.Put(ctx, key, bytes.NewReader(prep.Bytes), prep.MimeType); err != nil {
		return UploadResult{}, fmt.Errorf("upload: put: %w", err)
	}
	if err := s.Assets.Insert(ctx, asset); err != nil {
		_ = s.Store.Delete(ctx, key)
		return UploadResult{}, fmt.Errorf("upload: insert: %w", err)
	}
	url, err := s.Store.SignedGetURL(ctx, key, previewTTL)
	if err != nil {
		_ = s.Store.Delete(ctx, key)
		_ = s.Assets.SoftDelete(ctx, id, now)
		return UploadResult{}, fmt.Errorf("upload: signed url: %w", err)
	}
	return UploadResult{
		UploadID:            id,
		PreviewURL:          url,
		PreviewURLExpiresAt: now.Add(previewTTL),
		DeleteAfter:         deleteAfter,
		Width:               prep.Width,
		Height:              prep.Height,
		MimeType:            prep.MimeType,
	}, nil
}
