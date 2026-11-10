package ports

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
	"unicode"
)

// AssetKeyPrefix is the required key namespace for Metal Dogs private objects.
// Must stay under Shopanda media.PrivateBlobsPrefix so S3 Save stays private.
const AssetKeyPrefix = "private/blobs/metaldogs/"

// MaxSignedGetTTL caps bearer signed URLs for customer assets (ADR-0004).
const MaxSignedGetTTL = 15 * time.Minute

// DefaultSignedGetTTL is used when callers pass ttl <= 0.
const DefaultSignedGetTTL = 60 * time.Second

// AssetStore is the Metal Dogs private object-storage port (ADR-0004).
// Implementations must not expose permanent public URLs; use SignedGetURL.
// Keys must be under AssetKeyPrefix (enforced by the mediastore adapter).
type AssetStore interface {
	Put(ctx context.Context, key string, r io.Reader, contentType string) error
	// Read returns object bytes (C4 worker input). Requires a ReadableStorage backend.
	Read(ctx context.Context, key string) ([]byte, error)
	Head(ctx context.Context, key string) error
	SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	Delete(ctx context.Context, key string) error
}

// ObjectKey builds an opaque storage key:
// private/blobs/metaldogs/{asset_type}/{yyyy}/{mm}/{asset_id}.{ext}
// Keys never include dog names, customer IDs, or original filenames.
func ObjectKey(assetType, assetID, ext string, at time.Time) (string, error) {
	assetType = strings.TrimSpace(assetType)
	assetID = strings.TrimSpace(assetID)
	ext = strings.TrimPrefix(strings.TrimSpace(ext), ".")
	if assetType == "" || assetID == "" || ext == "" {
		return "", fmt.Errorf("ports: object key: asset_type, asset_id, and ext are required")
	}
	if err := validateKeySegment(assetType); err != nil {
		return "", fmt.Errorf("ports: object key asset_type: %w", err)
	}
	if err := validateKeySegment(assetID); err != nil {
		return "", fmt.Errorf("ports: object key asset_id: %w", err)
	}
	if err := validateKeySegment(ext); err != nil {
		return "", fmt.Errorf("ports: object key ext: %w", err)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	return path.Join(
		strings.TrimSuffix(AssetKeyPrefix, "/"),
		assetType,
		at.Format("2006"),
		at.Format("01"),
		assetID+"."+ext,
	), nil
}

// CapSignedGetTTL clamps ttl into (0, MaxSignedGetTTL]; zero/negative → DefaultSignedGetTTL.
func CapSignedGetTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return DefaultSignedGetTTL
	}
	if ttl > MaxSignedGetTTL {
		return MaxSignedGetTTL
	}
	return ttl
}

func validateKeySegment(s string) error {
	if strings.Contains(s, "/") || strings.Contains(s, `\`) || strings.Contains(s, "..") {
		return fmt.Errorf("invalid segment %q", s)
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("invalid character in %q", s)
	}
	return nil
}
