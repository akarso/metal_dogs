package ports

import (
	"context"
	"errors"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// ErrProductionAlreadyReady means the version is already ready; callers must not write again.
var ErrProductionAlreadyReady = errors.New("production already ready")

// ErrProductionNotOpen means the version is not pending or preparing, so ready/failed cannot be applied.
var ErrProductionNotOpen = errors.New("production status is not pending or preparing")

// DesignVersionRepository persists immutable DesignVersion rows.
type DesignVersionRepository interface {
	GetByID(ctx context.Context, id string) (domain.DesignVersion, bool, error)
	GetByDesignID(ctx context.Context, designID string) (domain.DesignVersion, bool, error)
	// ApproveBound locks the design (preview_ready), re-checks the artwork row, inserts
	// the version, marks the design approved, and slides retention clocks.
	// Requires d.ID == v.DesignID and d.ApprovedVersionID == v.ID.
	// Returns ErrDesignConflict when the design is no longer preview_ready or the
	// unique(design_id) insert races; callers re-read to distinguish approved vs other.
	ApproveBound(ctx context.Context, d domain.Design, v domain.DesignVersion) error
	// MarkProductionPreparing moves pending → preparing. Already preparing is a no-op.
	// Ready returns ErrProductionAlreadyReady. Any other status returns ErrProductionNotOpen.
	MarkProductionPreparing(ctx context.Context, id string) error
	// MarkProductionFailed moves pending or preparing → failed and stores reason once.
	// Already failed is a no-op and does not replace the stored reason.
	MarkProductionFailed(ctx context.Context, id, reason string) error
	// CommitProductionReady inserts asset and sets ready in one transaction.
	// Allowed only from pending or preparing. Already ready returns ErrProductionAlreadyReady
	// and does not insert.
	CommitProductionReady(ctx context.Context, id string, asset domain.Asset) error
}

// PrepareProductionEnqueue enqueues integration.sync.metaldogs.prepare_production_artwork after approve.
type PrepareProductionEnqueue func(ctx context.Context, designVersionID, designID string) error
