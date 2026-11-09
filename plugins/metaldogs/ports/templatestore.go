package ports

import (
	"context"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// TemplateStore persists versioned template catalog rows.
type TemplateStore interface {
	// InsertDraft inserts a new draft row (caller sets ID, Version, ContentHash, Status=draft).
	InsertDraft(ctx context.Context, t domain.Template) error
	// LatestBySlug returns the highest version for slug (any status).
	LatestBySlug(ctx context.Context, slug string) (domain.Template, bool, error)
	// GetBySlugVersion returns a specific version.
	GetBySlugVersion(ctx context.Context, slug string, version int) (domain.Template, bool, error)
	// LatestDraftBySlug returns the newest draft for slug, if any.
	LatestDraftBySlug(ctx context.Context, slug string) (domain.Template, bool, error)
	// PublishedBySlug returns the currently published version for slug, if any.
	PublishedBySlug(ctx context.Context, slug string) (domain.Template, bool, error)
	// SetStatus updates status (and published_at when publishing).
	SetStatus(ctx context.Context, id, status string) error
	// RetirePublishedExcept sets published rows for slug to retired, excluding id.
	RetirePublishedExcept(ctx context.Context, slug, exceptID string) error
}
