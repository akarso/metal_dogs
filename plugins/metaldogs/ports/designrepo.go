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
	// UpdateEditable updates texts/status/expires under editable-status + expected source +
	// expectedUpdatedAt predicates, and best-effort moves attached source delete_after.
	UpdateEditable(ctx context.Context, d domain.Design, expectedSourceAssetID string, expectedUpdatedAt time.Time, assetDeleteAfter time.Time) error
	// CreateBound inserts the design and binds source_asset_id → owner_ref=design.id in one txn
	// (also sets the asset delete_after to attached retention).
	CreateBound(ctx context.Context, d domain.Design, assetDeleteAfter time.Time) error
	// ReplaceSource locks the design (status/source/updated_at predicates), soft-deletes the
	// prior source when still active, binds the new source, and updates the design.
	// Returns the prior asset storage_key for post-commit blob delete (empty if none).
	ReplaceSource(ctx context.Context, d domain.Design, expectedOldAssetID string, expectedUpdatedAt time.Time, newAssetDeleteAfter time.Time) (oldStorageKey string, err error)
}
