package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

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

// InsertDraft inserts a draft template row (always status=draft).
func (s *TemplateStore) InsertDraft(ctx context.Context, t domain.Template) error {
	if t.ID == "" || t.Slug == "" || t.Version < 1 || t.ContentHash == "" {
		return fmt.Errorf("metaldogs templates: id, slug, version, content_hash required")
	}
	t.Status = domain.TemplateDraft
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
			COALESCE($17, now()), NULL
		)
	`, t.ID, t.Slug, domain.TemplateDraft, t.Genre, t.StyleCategory, t.Version, t.ContentHash,
		prompt, model, photo, texts, out,
		refAsset, thumbAsset, t.ReferencePatternPath, t.ThumbnailPath,
		nullTime(t.CreatedAt),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("metaldogs templates insert: %w", ports.ErrTemplateVersionConflict)
		}
		return fmt.Errorf("metaldogs templates insert: %w", err)
	}
	return nil
}

func (s *TemplateStore) LatestBySlug(ctx context.Context, slug string) (domain.Template, bool, error) {
	return s.scanOne(ctx, s.db, `
		SELECT `+templateColumns+`
		FROM metaldogs_templates
		WHERE slug = $1
		ORDER BY version DESC
		LIMIT 1
	`, slug)
}

func (s *TemplateStore) GetBySlugVersion(ctx context.Context, slug string, version int) (domain.Template, bool, error) {
	return s.scanOne(ctx, s.db, `
		SELECT `+templateColumns+`
		FROM metaldogs_templates
		WHERE slug = $1 AND version = $2
	`, slug, version)
}

func (s *TemplateStore) LatestDraftBySlug(ctx context.Context, slug string) (domain.Template, bool, error) {
	return s.scanOne(ctx, s.db, `
		SELECT `+templateColumns+`
		FROM metaldogs_templates
		WHERE slug = $1 AND status = $2
		ORDER BY version DESC
		LIMIT 1
	`, slug, domain.TemplateDraft)
}

func (s *TemplateStore) DraftBySlugHash(ctx context.Context, slug, contentHash string) (domain.Template, bool, error) {
	return s.scanOne(ctx, s.db, `
		SELECT `+templateColumns+`
		FROM metaldogs_templates
		WHERE slug = $1 AND status = $2 AND content_hash = $3
		ORDER BY version DESC
		LIMIT 1
	`, slug, domain.TemplateDraft, contentHash)
}

func (s *TemplateStore) PublishedBySlug(ctx context.Context, slug string) (domain.Template, bool, error) {
	return s.scanOne(ctx, s.db, `
		SELECT `+templateColumns+`
		FROM metaldogs_templates
		WHERE slug = $1 AND status = $2
		ORDER BY version DESC
		LIMIT 1
	`, slug, domain.TemplatePublished)
}

// publicTemplateColumns are the only columns loaded on guest list/get paths.
// Intentionally omits prompt_instructions, model_parameters, content_hash,
// reference/thumbnail asset ids and file paths.
const publicTemplateColumns = `
	id, slug, genre, style_category, version,
	photo_requirements, text_fields, output_config,
	published_at
`

func (s *TemplateStore) ListPublished(ctx context.Context, genre string) ([]ports.PublishedTemplate, error) {
	genre = strings.TrimSpace(genre)
	var (
		rows *sql.Rows
		err  error
	)
	if genre == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+publicTemplateColumns+`
			FROM metaldogs_templates
			WHERE status = $1
			ORDER BY slug ASC
		`, domain.TemplatePublished)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+publicTemplateColumns+`
			FROM metaldogs_templates
			WHERE status = $1 AND genre = $2
			ORDER BY slug ASC
		`, domain.TemplatePublished, genre)
	}
	if err != nil {
		return nil, fmt.Errorf("metaldogs templates list published: %w", err)
	}
	defer rows.Close()
	var out []ports.PublishedTemplate
	for rows.Next() {
		t, err := scanPublishedTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metaldogs templates list published: %w", err)
	}
	if out == nil {
		out = []ports.PublishedTemplate{}
	}
	return out, nil
}

func (s *TemplateStore) GetPublishedByID(ctx context.Context, id string) (ports.PublishedTemplate, bool, error) {
	if id == "" {
		return ports.PublishedTemplate{}, false, nil
	}
	t, err := scanPublishedTemplate(s.db.QueryRowContext(ctx, `
		SELECT `+publicTemplateColumns+`
		FROM metaldogs_templates
		WHERE id = $1 AND status = $2
	`, id, domain.TemplatePublished))
	if errors.Is(err, sql.ErrNoRows) {
		return ports.PublishedTemplate{}, false, nil
	}
	if err != nil {
		return ports.PublishedTemplate{}, false, err
	}
	return t, true, nil
}

// GetByID returns a template by id for any status (pinned design field validation).
func (s *TemplateStore) GetByID(ctx context.Context, id string) (domain.Template, bool, error) {
	if id == "" {
		return domain.Template{}, false, nil
	}
	return s.scanOne(ctx, s.db, `
		SELECT `+templateColumns+`
		FROM metaldogs_templates
		WHERE id = $1
	`, id)
}

func (s *TemplateStore) SetStatus(ctx context.Context, id, status string) error {
	if id == "" {
		return fmt.Errorf("metaldogs templates: id required")
	}
	var res sql.Result
	var err error
	switch status {
	case domain.TemplatePublished:
		res, err = s.db.ExecContext(ctx, `
			UPDATE metaldogs_templates
			SET status = $2, published_at = now()
			WHERE id = $1
		`, id, status)
	default:
		res, err = s.db.ExecContext(ctx, `
			UPDATE metaldogs_templates
			SET status = $2
			WHERE id = $1
		`, id, status)
	}
	if err != nil {
		return fmt.Errorf("metaldogs templates set status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("metaldogs templates set status rows: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("metaldogs templates: id %q not found", id)
	}
	return nil
}

// PublishExclusive retires other published and other drafts for slug, then publishes id.
func (s *TemplateStore) PublishExclusive(ctx context.Context, slug, id string) (domain.Template, error) {
	if slug == "" || id == "" {
		return domain.Template{}, fmt.Errorf("metaldogs templates: slug and id required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Template{}, fmt.Errorf("metaldogs templates begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE metaldogs_templates
		SET status = $3
		WHERE slug = $1 AND status = $2 AND id <> $4
	`, slug, domain.TemplatePublished, domain.TemplateRetired, id); err != nil {
		return domain.Template{}, fmt.Errorf("metaldogs templates retire published: %w", err)
	}
	// Stale drafts must not remain selectable by LatestDraftBySlug after publish.
	if _, err := tx.ExecContext(ctx, `
		UPDATE metaldogs_templates
		SET status = $3
		WHERE slug = $1 AND status = $2 AND id <> $4
	`, slug, domain.TemplateDraft, domain.TemplateRetired, id); err != nil {
		return domain.Template{}, fmt.Errorf("metaldogs templates retire drafts: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE metaldogs_templates
		SET
			published_at = CASE WHEN status = 'published' THEN published_at ELSE now() END,
			status = $3
		WHERE id = $1 AND slug = $2
	`, id, slug, domain.TemplatePublished)
	if err != nil {
		return domain.Template{}, fmt.Errorf("metaldogs templates publish: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return domain.Template{}, err
	}
	if n == 0 {
		return domain.Template{}, fmt.Errorf("metaldogs templates: id %q slug %q not found", id, slug)
	}
	t, ok, err := s.scanOne(ctx, tx, `
		SELECT `+templateColumns+`
		FROM metaldogs_templates
		WHERE id = $1 AND slug = $2
	`, id, slug)
	if err != nil {
		return domain.Template{}, err
	}
	if !ok {
		return domain.Template{}, fmt.Errorf("metaldogs templates: id %q missing after publish", id)
	}
	if err := tx.Commit(); err != nil {
		return domain.Template{}, fmt.Errorf("metaldogs templates commit: %w", err)
	}
	return t, nil
}

// RetireExclusive retires all published rows for slug and fails if any published remain.
func (s *TemplateStore) RetireExclusive(ctx context.Context, slug string) (domain.Template, error) {
	if slug == "" {
		return domain.Template{}, fmt.Errorf("metaldogs templates: slug required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Template{}, fmt.Errorf("metaldogs templates begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		UPDATE metaldogs_templates
		SET status = $2
		WHERE slug = $1 AND status = $3
		RETURNING `+templateColumns+`
	`, slug, domain.TemplateRetired, domain.TemplatePublished)
	if err != nil {
		return domain.Template{}, fmt.Errorf("metaldogs templates retire: %w", err)
	}
	defer rows.Close()

	var retired []domain.Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return domain.Template{}, err
		}
		retired = append(retired, t)
	}
	if err := rows.Err(); err != nil {
		return domain.Template{}, err
	}
	if len(retired) == 0 {
		return domain.Template{}, fmt.Errorf("metaldogs templates: no published template for slug %q", slug)
	}

	var still int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM metaldogs_templates WHERE slug = $1 AND status = $2
	`, slug, domain.TemplatePublished).Scan(&still); err != nil {
		return domain.Template{}, fmt.Errorf("metaldogs templates retire verify: %w", err)
	}
	if still != 0 {
		return domain.Template{}, fmt.Errorf("metaldogs templates: slug %q still has %d published row(s) after retire", slug, still)
	}
	if err := tx.Commit(); err != nil {
		return domain.Template{}, fmt.Errorf("metaldogs templates commit: %w", err)
	}
	// Highest version among those just retired (stable for CLI messaging).
	best := retired[0]
	for _, r := range retired[1:] {
		if r.Version > best.Version {
			best = r
		}
	}
	return best, nil
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const templateColumns = `
	id, slug, status, genre, style_category, version, content_hash,
	prompt_instructions, model_parameters, photo_requirements, text_fields, output_config,
	reference_asset_id, thumbnail_asset_id, reference_pattern_path, thumbnail_path,
	created_at, published_at
`

type scannable interface {
	Scan(dest ...any) error
}

func (s *TemplateStore) scanOne(ctx context.Context, q rowQuerier, query string, args ...any) (domain.Template, bool, error) {
	t, err := scanTemplate(q.QueryRowContext(ctx, query, args...))
	if err == sql.ErrNoRows {
		return domain.Template{}, false, nil
	}
	if err != nil {
		return domain.Template{}, false, err
	}
	return t, true, nil
}

func scanPublishedTemplate(row scannable) (ports.PublishedTemplate, error) {
	var id, slug, genre, style string
	var version int
	var photo, texts, out []byte
	var publishedAt sql.NullTime
	err := row.Scan(
		&id, &slug, &genre, &style, &version,
		&photo, &texts, &out,
		&publishedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ports.PublishedTemplate{}, err
		}
		return ports.PublishedTemplate{}, fmt.Errorf("metaldogs templates scan public: %w", err)
	}
	// Unmarshal jsonb via domain shapes (DB contract), then copy into port-owned types.
	var photoD domain.PhotoRequirements
	var textsD domain.TextFields
	var outD domain.OutputConfig
	if err := json.Unmarshal(photo, &photoD); err != nil {
		return ports.PublishedTemplate{}, err
	}
	if err := json.Unmarshal(texts, &textsD); err != nil {
		return ports.PublishedTemplate{}, err
	}
	if err := json.Unmarshal(out, &outD); err != nil {
		return ports.PublishedTemplate{}, err
	}
	rowT := domain.Template{
		ID: id, Slug: slug, Genre: genre, StyleCategory: style, Version: version,
		PhotoRequirements: photoD, TextFields: textsD, OutputConfig: outD,
	}
	if publishedAt.Valid {
		ts := publishedAt.Time.UTC()
		rowT.PublishedAt = &ts
	}
	return ports.PublishedFromDomain(rowT), nil
}

func scanTemplate(row scannable) (domain.Template, error) {
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
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Template{}, err
		}
		return domain.Template{}, fmt.Errorf("metaldogs templates scan: %w", err)
	}
	if err := json.Unmarshal(prompt, &t.PromptInstructions); err != nil {
		return domain.Template{}, err
	}
	if err := json.Unmarshal(model, &t.ModelParameters); err != nil {
		return domain.Template{}, err
	}
	if err := json.Unmarshal(photo, &t.PhotoRequirements); err != nil {
		return domain.Template{}, err
	}
	if err := json.Unmarshal(texts, &t.TextFields); err != nil {
		return domain.Template{}, err
	}
	if err := json.Unmarshal(out, &t.OutputConfig); err != nil {
		return domain.Template{}, err
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
	t.NormalizeForHash()
	return t, nil
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
