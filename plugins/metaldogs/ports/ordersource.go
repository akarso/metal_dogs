package ports

import (
	"context"
	"time"
)

// OrderLine is one order line as the snapshot job sees it.
// DesignID is empty when the line has no metaldogs.design_id.
type OrderLine struct {
	VariantID string
	SKU       string
	Quantity  int
	DesignID  string
}

// OrderSource is the plugin-owned view of orders. E4a has no core adapter.
type OrderSource interface {
	Lines(ctx context.Context, orderID string) ([]OrderLine, error)
	// ListCreatedSince returns order ids with created_at >= since, id > afterID,
	// ordered by id, at most limit. limit <= 0 returns no ids.
	ListCreatedSince(ctx context.Context, since time.Time, afterID string, limit int) ([]string, error)
}
