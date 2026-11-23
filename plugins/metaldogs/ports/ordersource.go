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

// OrderSource is the plugin-owned view of orders.
type OrderSource interface {
	Lines(ctx context.Context, orderID string) ([]OrderLine, error)
	// LinesForOrders returns lines for each order id the source has.
	// Missing ids are omitted. One call covers a page of ids.
	LinesForOrders(ctx context.Context, orderIDs []string) (map[string][]OrderLine, error)
	// ListCreatedSince returns order ids with created_at >= since, ordered by
	// (created_at, id). afterID continues after that order and must itself
	// fall inside the since window. limit must be 1..100.
	ListCreatedSince(ctx context.Context, since time.Time, afterID string, limit int) ([]string, error)
}
