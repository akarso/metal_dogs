package ports

import (
	"context"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

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
}

// PrepareProductionEnqueue enqueues integration.sync.metaldogs.prepare_production_artwork after approve.
type PrepareProductionEnqueue func(ctx context.Context, designVersionID, designID string) error
