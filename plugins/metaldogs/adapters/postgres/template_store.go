package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// TemplateStore is a Postgres adapter for ports.TemplateStore.
type TemplateStore struct {
	db *sql.DB
}

// NewTemplateStore returns a TemplateStore backed by db.
func NewTemplateStore(db *sql.DB) *TemplateStore {
	return &TemplateStore{db: db}
}

var _ ports.TemplateStore = (*TemplateStore)(nil)

// InsertDraft inserts a draft template row.
func (s *TemplateStore) InsertDraft(ctx context.Context, t domain.Template) error {
	if t.ID == "" || t.Slug == "" || t.Version < 1 || t.ContentHash == "" {
		return fmt.Errorf("metaldogs templates: id, slug, version, content_hash required")
	}
	if t.Status == "" {
		t.Status = domain.TemplateDraft
	}
	prompt, err := json.Marshal(t.PromptInstructions)
	if err != nil {
		return err
	}
	model, err := json.Marshal(t.ModelParameters)
	if err != nil {
		return err
	}
	photo, err := json.Marshal(t.PhotoRequirements)
	if err != nil {
		return err
	}
	texts, err := json.Marshal(t.TextFields)
	if err != nil {
		return err
	}
	out, err := json.Marshal(t.OutputConfig)
	if err != nil {
		return err
	}
	var refAsset, thumbAsset any
	if t.ReferenceAssetID != "" {
		refAsset = t.ReferenceAssetID
	}
	if t.ThumbnailAssetID != "" {
		thumbAsset = t.ThumbnailAssetID
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO metaldogs_templates (
			id, slug, status, genre, style_category, version, content_hash,
			prompt_instructions, model_parameters, photo_requirements, text_fields, output_config,
			reference_asset_id, thumbnail_asset_id, reference_pattern_path, thumbnail_path,
			created_at, published_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,
			$8::jsonb,$9::jsonb,$10::jsonb,$11::jsonb,$12::jsonb,
			$13,$14,$15,$16,
			COALESCE($17, now()), $18
		)
	`, t.ID, t.Slug, t.Status, t.Genre, t.StyleCategory, t.Version, t.ContentHash,
		prompt, model, photo, texts, out,
		refAsset, thumbAsset, t.ReferencePatternPath, t.ThumbnailPath,
		nullTime(t.CreatedAt), nullTimePtr(t.PublishedAt),
	)
	if err != nil {
		return fmt.Errorf("metaldogs templates insert: %w", err)
	}
	return nil
}

func (s *TemplateStore) LatestBySlug(ctx context.Context, slug string) (domain.Template, bool, error) {
	return s.scanOne(ctx, `
		SELECT `+templateColumns+`
		FROM metaldogs_templates
		WHERE slug = $1
		ORDER BY version DESC
		LIMIT 1
	`, slug)
}

func (s *TemplateStore) GetBySlugVersion(ctx context.Context, slug string, version int) (domain.Template, bool, error) {
	return s.scanOne(ctx, `
		SELECT `+templateColumns+`
		FROM metaldogs_templates
		WHERE slug = $1 AND version = $2
	`, slug, version)
}

func (s *TemplateStore) LatestDraftBySlug(ctx context.Context, slug string) (domain.Template, bool, error) {
	return s.scanOne(ctx, `
		SELECT `+templateColumns+`
		FROM metaldogs_templates
		WHERE slug = $1 AND status = $2
		ORDER BY version DESC
		LIMIT 1
	`, slug, domain.TemplateDraft)
}

func (s *TemplateStore) PublishedBySlug(ctx context.Context, slug string) (domain.Template, bool, error) {
	return s.scanOne(ctx, `
		SELECT `+templateColumns+`
		FROM metaldogs_templates
		WHERE slug = $1 AND status = $2
		ORDER BY version DESC
		LIMIT 1
	`, slug, domain.TemplatePublished)
}

func (s *TemplateStore) SetStatus(ctx context.Context, id, status string) error {
	if id == "" {
		return fmt.Errorf("metaldogs templates: id required")
	}
	var err error
	switch status {
	case domain.TemplatePublished:
		_, err = s.db.ExecContext(ctx, `
			UPDATE metaldogs_templates
			SET status = $2, published_at = now()
			WHERE id = $1
		`, id, status)
	default:
		_, err = s.db.ExecContext(ctx, `
			UPDATE metaldogs_templates
			SET status = $2
			WHERE id = $1
		`, id, status)
	}
	if err != nil {
		return fmt.Errorf("metaldogs templates set status: %w", err)
	}
	return nil
}

func (s *TemplateStore) RetirePublishedExcept(ctx context.Context, slug, exceptID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE metaldogs_templates
		SET status = $3
		WHERE slug = $1 AND status = $2 AND id <> $4
	`, slug, domain.TemplatePublished, domain.TemplateRetired, exceptID)
	if err != nil {
		return fmt.Errorf("metaldogs templates retire published: %w", err)
	}
	return nil
}

const templateColumns = `
	id, slug, status, genre, style_category, version, content_hash,
	prompt_instructions, model_parameters, photo_requirements, text_fields, output_config,
	reference_asset_id, thumbnail_asset_id, reference_pattern_path, thumbnail_path,
	created_at, published_at
`

func (s *TemplateStore) scanOne(ctx context.Context, query string, args ...any) (domain.Template, bool, error) {
	row := s.db.QueryRowContext(ctx, query, args...)
	var t domain.Template
	var prompt, model, photo, texts, out []byte
	var refAsset, thumbAsset sql.NullString
	var publishedAt sql.NullTime
	err := row.Scan(
		&t.ID, &t.Slug, &t.Status, &t.Genre, &t.StyleCategory, &t.Version, &t.ContentHash,
		&prompt, &model, &photo, &texts, &out,
		&refAsset, &thumbAsset, &t.ReferencePatternPath, &t.ThumbnailPath,
		&t.CreatedAt, &publishedAt,
	)
	if err == sql.ErrNoRows {
		return domain.Template{}, false, nil
	}
	if err != nil {
		return domain.Template{}, false, fmt.Errorf("metaldogs templates scan: %w", err)
	}
	if err := json.Unmarshal(prompt, &t.PromptInstructions); err != nil {
		return domain.Template{}, false, err
	}
	if err := json.Unmarshal(model, &t.ModelParameters); err != nil {
		return domain.Template{}, false, err
	}
	if err := json.Unmarshal(photo, &t.PhotoRequirements); err != nil {
		return domain.Template{}, false, err
	}
	if err := json.Unmarshal(texts, &t.TextFields); err != nil {
		return domain.Template{}, false, err
	}
	if err := json.Unmarshal(out, &t.OutputConfig); err != nil {
		return domain.Template{}, false, err
	}
	if refAsset.Valid {
		t.ReferenceAssetID = refAsset.String
	}
	if thumbAsset.Valid {
		t.ThumbnailAssetID = thumbAsset.String
	}
	if publishedAt.Valid {
		ts := publishedAt.Time.UTC()
		t.PublishedAt = &ts
	}
	return t, true, nil
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

func nullTimePtr(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC()
}
