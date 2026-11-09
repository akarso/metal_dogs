package ports

import (
	"context"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// TemplateStore persists versioned template catalog rows.
type TemplateStore interface {
	// InsertDraft inserts a new draft row (forces status=draft).
	InsertDraft(ctx context.Context, t domain.Template) error
	// LatestBySlug returns the highest version for slug (any status).
	LatestBySlug(ctx context.Context, slug string) (domain.Template, bool, error)
	// GetBySlugVersion returns a specific version.
	GetBySlugVersion(ctx context.Context, slug string, version int) (domain.Template, bool, error)
	// LatestDraftBySlug returns the newest draft for slug, if any.
	LatestDraftBySlug(ctx context.Context, slug string) (domain.Template, bool, error)
	// DraftBySlugHash returns a draft for slug with the given content hash, if any.
	DraftBySlugHash(ctx context.Context, slug, contentHash string) (domain.Template, bool, error)
	// PublishedBySlug returns the currently published version for slug, if any.
	PublishedBySlug(ctx context.Context, slug string) (domain.Template, bool, error)
	// SetStatus updates status (and published_at when publishing). Errors if id missing.
	SetStatus(ctx context.Context, id, status string) error
	// PublishExclusive retires other published + other drafts for slug and publishes id in one transaction.
	PublishExclusive(ctx context.Context, slug, id string) (domain.Template, error)
}
