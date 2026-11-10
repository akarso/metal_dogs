package ports

import (
	"context"
	"errors"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// ErrAssetBindConflict means the asset could not be bound (missing, wrong type, or already owned).
var ErrAssetBindConflict = errors.New("asset bind conflict")

// DesignRepository persists Design aggregates.
type DesignRepository interface {
	Insert(ctx context.Context, d domain.Design) error
	GetByID(ctx context.Context, id string) (domain.Design, bool, error)
	UpdateEditable(ctx context.Context, d domain.Design) error
	// CreateBound inserts the design and binds source_asset_id → owner_ref=design.id in one txn.
	CreateBound(ctx context.Context, d domain.Design) error
	// ReplaceSource clears oldAssetID owner_ref, binds newAssetID, and updates the design row.
	ReplaceSource(ctx context.Context, d domain.Design, oldAssetID string) error
}
