package application

import (
	"context"
	"errors"
	"strings"

	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/thumbnail"
)

// ErrOrderThumbnailMiss is a thumbnail the public handler answers as not found.
// The cause is not wrapped, so a storage key cannot leak through the error text.
var ErrOrderThumbnailMiss = errors.New("order thumbnail not found")

const (
	// OrderThumbnailCauseSnapshot is a snapshot repository error.
	OrderThumbnailCauseSnapshot = "snapshot"
	// OrderThumbnailCauseAsset is an asset repository error.
	OrderThumbnailCauseAsset = "asset"
	// OrderThumbnailCauseObject is an asset-store read error.
	OrderThumbnailCauseObject = "object"
)

// OrderThumbnailFault is a snapshot, asset, or object read failure.
// Error text is fixed so a storage key in the cause cannot leak.
// The public handler still answers 404.
type OrderThumbnailFault struct {
	OrderID   string
	VariantID string
	Cause     string
	Err       error
}

func (e *OrderThumbnailFault) Error() string { return "order thumbnail unavailable" }

func (e *OrderThumbnailFault) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

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
// A snapshot, asset, or object read error is *OrderThumbnailFault.
func (s *OrderThumbnail) Bytes(ctx context.Context, token string) ([]byte, string, error) {
	if s == nil || s.Snapshots == nil || s.Assets == nil || s.Store == nil || len(s.Key) < 32 {
		return nil, "", ErrOrderThumbnailMiss
	}
	orderID, variantID, ok := thumbnail.Verify(s.Key, token)
	if !ok {
		return nil, "", ErrOrderThumbnailMiss
	}
	snap, found, err := s.Snapshots.Get(ctx, orderID, variantID)
	if err != nil {
		return nil, "", &OrderThumbnailFault{OrderID: orderID, VariantID: variantID, Cause: OrderThumbnailCauseSnapshot, Err: err}
	}
	if !found || snap.ThumbnailAssetID == "" {
		return nil, "", ErrOrderThumbnailMiss
	}
	asset, found, err := s.Assets.GetByID(ctx, snap.ThumbnailAssetID)
	if err != nil {
		return nil, "", &OrderThumbnailFault{OrderID: orderID, VariantID: variantID, Cause: OrderThumbnailCauseAsset, Err: err}
	}
	if !found || asset.StorageKey == "" {
		return nil, "", ErrOrderThumbnailMiss
	}
	mime, ok := orderThumbnailMIME[strings.ToLower(strings.TrimSpace(asset.MimeType))]
	if !ok {
		return nil, "", ErrOrderThumbnailMiss
	}
	body, err := s.Store.Read(ctx, asset.StorageKey)
	if err != nil {
		return nil, "", &OrderThumbnailFault{OrderID: orderID, VariantID: variantID, Cause: OrderThumbnailCauseObject, Err: err}
	}
	return body, mime, nil
}
