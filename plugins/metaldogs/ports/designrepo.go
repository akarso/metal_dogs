package ports

import (
	"context"
	"errors"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// ErrAssetBindConflict means the asset could not be bound (missing, wrong type, or already owned).
var ErrAssetBindConflict = errors.New("asset bind conflict")

// ErrDesignConflict means a conditional design update lost a race or hit a non-editable status.
var ErrDesignConflict = errors.New("design conflict")

// DesignRepository persists Design aggregates.
type DesignRepository interface {
	Insert(ctx context.Context, d domain.Design) error
	GetByID(ctx context.Context, id string) (domain.Design, bool, error)
	// UpdateEditable updates texts/status/expires under editable-status + expected source predicates
	// and moves the attached source delete_after.
	UpdateEditable(ctx context.Context, d domain.Design, expectedSourceAssetID string, assetDeleteAfter time.Time) error
	// CreateBound inserts the design and binds source_asset_id → owner_ref=design.id in one txn
	// (also sets the asset delete_after to attached retention).
	CreateBound(ctx context.Context, d domain.Design, assetDeleteAfter time.Time) error
	// ReplaceSource locks the design, soft-deletes expectedOldAssetID, binds the new source,
	// and updates the design when still editable with the expected prior source_asset_id.
	ReplaceSource(ctx context.Context, d domain.Design, expectedOldAssetID string, newAssetDeleteAfter time.Time) error
	// TouchActivity extends design expires_at and attached source delete_after.
	TouchActivity(ctx context.Context, designID string, now, expiresAt time.Time, sourceAssetID string, assetDeleteAfter time.Time) error
}
