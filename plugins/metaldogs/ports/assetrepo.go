package ports

import (
	"context"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// AssetRepository persists Asset metadata (not blob bytes).
type AssetRepository interface {
	Insert(ctx context.Context, a domain.Asset) error
	// GetByID returns an active asset (deleted_at IS NULL).
	GetByID(ctx context.Context, id string) (domain.Asset, bool, error)
	// SoftDelete marks an asset deleted (compensating cleanup / retention).
	SoftDelete(ctx context.Context, id string, at time.Time) error
}
