package metaldogs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/thumbnail"
)

func (p *Plugin) openOrderThumbnail() (*application.OrderThumbnail, error) {
	db, err := pluginDB(p)
	if err != nil {
		return nil, err
	}
	if p.assets == nil || len(p.thumbnailKey) < 32 {
		return nil, fmt.Errorf("metaldogs: order thumbnail is not configured")
	}
	return &application.OrderThumbnail{
		Snapshots: postgres.NewOrderDesignSnapshotRepo(db),
		Assets:    postgres.NewAssetRepo(db),
		Store:     p.assets,
		Key:       p.thumbnailKey,
	}, nil
}

// OrderThumbnailURL is the stable public link for one order line.
// It is empty when order snapshots are off, the signing key is absent, or the line has no thumbnail.
// Nothing in email or storefront pages calls it.
func (p *Plugin) OrderThumbnailURL(ctx context.Context, orderID, variantID string) (string, error) {
	if p == nil || !p.orderSnapshotEnabled || len(p.thumbnailKey) < 32 {
		return "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	db, err := pluginDB(p)
	if err != nil {
		return "", err
	}
	snap, found, err := postgres.NewOrderDesignSnapshotRepo(db).Get(ctx, strings.TrimSpace(orderID), strings.TrimSpace(variantID))
	if err != nil {
		if errors.Is(err, ports.ErrInvalidOrderID) {
			return "", nil
		}
		return "", err
	}
	if !found || snap.ThumbnailAssetID == "" {
		return "", nil
	}
	if p.publicBaseURL == "" {
		return "", fmt.Errorf("metaldogs: plugins.metaldogs.public_base_url is empty")
	}
	token := thumbnail.Token(p.thumbnailKey, snap.OrderID, snap.VariantID)
	return p.publicBaseURL + "/api/customizer/order-thumbnails/" + token, nil
}
