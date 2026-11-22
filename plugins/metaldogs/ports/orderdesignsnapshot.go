package ports

import (
	"context"
	"errors"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// ErrSnapshotNotFound means no OrderDesignSnapshot exists for the line key.
var ErrSnapshotNotFound = errors.New("order design snapshot not found")

// OrderDesignSnapshotRepository stores one immutable snapshot per order line.
type OrderDesignSnapshotRepository interface {
	// Insert writes the row. A duplicate (order_id, variant_id) writes nothing
	// and returns written=false.
	Insert(ctx context.Context, s domain.OrderDesignSnapshot) (written bool, err error)
	Get(ctx context.Context, orderID, variantID string) (domain.OrderDesignSnapshot, bool, error)
	ListByOrder(ctx context.Context, orderID string) ([]domain.OrderDesignSnapshot, error)
	// FillProduction sets production artwork once, while both columns are NULL.
	// A second call returns domain.ErrProductionAlreadySet.
	FillProduction(ctx context.Context, orderID, variantID, assetID, checksum string) error
}
