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

// AssetStore is the Metal Dogs private object-storage port (ADR-0004).
// Implementations must not expose permanent public URLs; use SignedGetURL.
type AssetStore interface {
	Put(ctx context.Context, key string, r io.Reader, contentType string) error
	Head(ctx context.Context, key string) error
	SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	Delete(ctx context.Context, key string) error
}

// ObjectKey builds an opaque storage key:
// metaldogs/{asset_type}/{yyyy}/{mm}/{asset_id}.{ext}
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
		"metaldogs",
		assetType,
		at.Format("2006"),
		at.Format("01"),
		assetID+"."+ext,
	), nil
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
