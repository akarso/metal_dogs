package application

import (
	"context"
	"errors"
	"strings"

	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/thumbnail"
)

// ErrOrderThumbnailMiss is every thumbnail read failure the public handler may show.
// The cause is not wrapped, so a storage key cannot leak through the error text.
var ErrOrderThumbnailMiss = errors.New("order thumbnail not found")

var orderThumbnailMIME = map[string]string{
	"image/jpeg": "image/jpeg",
	"image/png":  "image/png",
	"image/webp": "image/webp",
}

// OrderThumbnail reads one order line's thumbnail bytes for a signed token.
type OrderThumbnail struct {
	Snapshots ports.OrderDesignSnapshotRepository
	Assets    ports.AssetRepository
	Store     ports.AssetStore
	Key       string
}

// Bytes returns the thumbnail and an allowlisted MIME type.
// A bad token, unknown line, missing thumbnail, or missing object is ErrOrderThumbnailMiss.
func (s *OrderThumbnail) Bytes(ctx context.Context, token string) ([]byte, string, error) {
	if s == nil || s.Snapshots == nil || s.Assets == nil || s.Store == nil || len(s.Key) < 32 {
		return nil, "", ErrOrderThumbnailMiss
	}
	orderID, variantID, ok := thumbnail.Verify(s.Key, token)
	if !ok {
		return nil, "", ErrOrderThumbnailMiss
	}
	snap, found, err := s.Snapshots.Get(ctx, orderID, variantID)
	if err != nil || !found || snap.ThumbnailAssetID == "" {
		return nil, "", ErrOrderThumbnailMiss
	}
	asset, found, err := s.Assets.GetByID(ctx, snap.ThumbnailAssetID)
	if err != nil || !found || asset.StorageKey == "" {
		return nil, "", ErrOrderThumbnailMiss
	}
	mime, ok := orderThumbnailMIME[strings.ToLower(strings.TrimSpace(asset.MimeType))]
	if !ok {
		return nil, "", ErrOrderThumbnailMiss
	}
	body, err := s.Store.Read(ctx, asset.StorageKey)
	if err != nil {
		return nil, "", ErrOrderThumbnailMiss
	}
	return body, mime, nil
}
