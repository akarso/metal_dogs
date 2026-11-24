package ports

import (
	"context"
	"errors"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// ErrSnapshotNotFound means no OrderDesignSnapshot exists for the line key.
var ErrSnapshotNotFound = errors.New("order design snapshot not found")

// ErrInvalidOrderID means orderID is not a UUID, so it is not queried.
var ErrInvalidOrderID = errors.New("order id is not a uuid")

// SnapshotDesignError is a CommitOrder failure tied to one design.
// DesignID is empty when the failing line is not known.
type SnapshotDesignError struct {
	DesignID string
	Err      error
}

func (e *SnapshotDesignError) Error() string {
	if e == nil || e.Err == nil {
		return "snapshot design failed"
	}
	return e.Err.Error()
}

func (e *SnapshotDesignError) Unwrap() error { return e.Err }

// OrderDesignSnapshotRepository stores one immutable snapshot per order line.
type OrderDesignSnapshotRepository interface {
	// Insert writes the row. A duplicate (order_id, variant_id) writes nothing
	// and returns written=false.
	Insert(ctx context.Context, s domain.OrderDesignSnapshot) (written bool, err error)
	// CommitOrder inserts every snapshot and moves each design from approved
	// to ordered in one transaction. thumbnails[i] is stored in that same
	// transaction when its ID is set; a nil or empty slice stores none.
	// written[i] is false when that line already existed.
	// thumbnailDropped[i] is true when that thumbnail was presented and not stored
	// (insert conflict, or the mockup is no longer live). The snapshot row is still written.
	// Any error rolls the transaction back.
	CommitOrder(ctx context.Context, snapshots []domain.OrderDesignSnapshot, thumbnails []domain.Asset, at time.Time) (written []bool, thumbnailDropped []bool, err error)
	Get(ctx context.Context, orderID, variantID string) (domain.OrderDesignSnapshot, bool, error)
	ListByOrder(ctx context.Context, orderID string) ([]domain.OrderDesignSnapshot, error)
	// ListUnfilledProduction returns snapshots of designVersionID whose production columns are NULL.
	ListUnfilledProduction(ctx context.Context, designVersionID string) ([]domain.OrderDesignSnapshot, error)
	// FillProduction sets production artwork once, while both columns are NULL.
	// A second call returns domain.ErrProductionAlreadySet.
	FillProduction(ctx context.Context, orderID, variantID, assetID, checksum string) error
}
