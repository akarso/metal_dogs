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

// DesignRepo is a Postgres adapter for ports.DesignRepository.
type DesignRepo struct {
	db *sql.DB
}

// NewDesignRepo returns a DesignRepo backed by db.
func NewDesignRepo(db *sql.DB) *DesignRepo {
	return &DesignRepo{db: db}
}

var _ ports.DesignRepository = (*DesignRepo)(nil)

const designColumns = `
	id, template_id, template_version, status, texts, source_asset_id, owner_token_hash,
	customer_id, generation_attempts, latest_job_id, approved_version_id,
	created_at, updated_at, expires_at
`

const editableStatusesSQL = `('draft', 'generation_failed', 'preview_ready')`

// Insert persists a design row (no asset bind).
func (r *DesignRepo) Insert(ctx context.Context, d domain.Design) error {
	return insertDesign(ctx, r.db, d)
}

// GetByID loads a design by id.
func (r *DesignRepo) GetByID(ctx context.Context, id string) (domain.Design, bool, error) {
	if id == "" {
		return domain.Design{}, false, nil
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT `+designColumns+`
		FROM metaldogs_designs WHERE id = $1
	`, id)
	return scanDesign(row)
}

// UpdateEditable updates texts/status/expires when still editable with expected source + updated_at.
func (r *DesignRepo) UpdateEditable(ctx context.Context, d domain.Design, expectedSourceAssetID string, expectedUpdatedAt time.Time, assetDeleteAfter time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metaldogs designs begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := updateEditable(ctx, tx, d, expectedSourceAssetID, expectedUpdatedAt); err != nil {
		return err
	}
	// Best-effort: source may already be soft-deleted by retention; text edits still commit.
	_ = touchAssetDeleteAfter(ctx, tx, d.SourceAssetID, assetDeleteAfter)
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("metaldogs designs update commit: %w", err)
	}
	return nil
}

// CreateBound inserts the design and binds the source asset in one transaction.
func (r *DesignRepo) CreateBound(ctx context.Context, d domain.Design, assetDeleteAfter time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metaldogs designs begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertDesign(ctx, tx, d); err != nil {
		return err
	}
	if err := bindOwnerRef(ctx, tx, d.SourceAssetID, d.ID, assetDeleteAfter); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("metaldogs designs commit: %w", err)
	}
	return nil
}

// ReplaceSource locks the design, soft-deletes the prior source, binds the new one, updates the row.
func (r *DesignRepo) ReplaceSource(ctx context.Context, d domain.Design, expectedOldAssetID string, expectedUpdatedAt time.Time, newAssetDeleteAfter time.Time) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("metaldogs designs begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var curStatus, curSource string
	var curUpdated time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT status, source_asset_id, updated_at FROM metaldogs_designs WHERE id = $1 FOR UPDATE
	`, d.ID).Scan(&curStatus, &curSource, &curUpdated)
	if err == sql.ErrNoRows {
		return "", ports.ErrDesignConflict
	}
	if err != nil {
		return "", fmt.Errorf("metaldogs designs lock: %w", err)
	}
	if !(domain.Design{Status: curStatus}).EditableForPatch() {
		return "", ports.ErrDesignConflict
	}
	if curSource != expectedOldAssetID {
		return "", ports.ErrDesignConflict
	}
	if !curUpdated.UTC().Equal(expectedUpdatedAt.UTC()) {
		return "", ports.ErrDesignConflict
	}

	oldKey := ""
	if expectedOldAssetID != "" && expectedOldAssetID != d.SourceAssetID {
		key, err := assetStorageKey(ctx, tx, expectedOldAssetID)
		if err != nil {
			return "", err
		}
		oldKey = key
		if err := softDeleteAsset(ctx, tx, expectedOldAssetID, d.UpdatedAt); err != nil {
			return "", err
		}
	}
	if err := bindOwnerRef(ctx, tx, d.SourceAssetID, d.ID, newAssetDeleteAfter); err != nil {
		return "", err
	}
	if err := updateEditable(ctx, tx, d, expectedOldAssetID, expectedUpdatedAt); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("metaldogs designs commit: %w", err)
	}
	return oldKey, nil
}

type designExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type designQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func insertDesign(ctx context.Context, db designExecer, d domain.Design) error {
	if d.ID == "" || d.TemplateID == "" || d.TemplateVersion < 1 || d.Status == "" ||
		d.SourceAssetID == "" || len(d.OwnerTokenHash) == 0 || d.ExpiresAt.IsZero() {
		return fmt.Errorf("metaldogs designs: required fields missing")
	}
	texts, err := json.Marshal(d.Texts)
	if err != nil {
		return err
	}
	var customer, latestJob, approved any
	if d.CustomerID != "" {
		customer = d.CustomerID
	}
	if d.LatestJobID != "" {
		latestJob = d.LatestJobID
	}
	if d.ApprovedVersionID != "" {
		approved = d.ApprovedVersionID
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO metaldogs_designs (
			id, template_id, template_version, status, texts, source_asset_id, owner_token_hash,
			customer_id, generation_attempts, latest_job_id, approved_version_id,
			created_at, updated_at, expires_at
		) VALUES (
			$1,$2,$3,$4,$5::jsonb,$6,$7,
			$8,$9,$10,$11,
			COALESCE($12, now()), COALESCE($13, now()), $14
		)
	`, d.ID, d.TemplateID, d.TemplateVersion, d.Status, texts, d.SourceAssetID, d.OwnerTokenHash,
		customer, d.GenerationAttempts, latestJob, approved,
		nullTime(d.CreatedAt), nullTime(d.UpdatedAt), d.ExpiresAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("metaldogs designs insert: %w", err)
	}
	return nil
}

func updateEditable(ctx context.Context, db designExecer, d domain.Design, expectedSourceAssetID string, expectedUpdatedAt time.Time) error {
	texts, err := json.Marshal(d.Texts)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `
		UPDATE metaldogs_designs
		SET texts = $2::jsonb, source_asset_id = $3, status = $4, updated_at = $5, expires_at = $6
		WHERE id = $1
		  AND status IN `+editableStatusesSQL+`
		  AND source_asset_id = $7
		  AND updated_at = $8
	`, d.ID, texts, d.SourceAssetID, d.Status, d.UpdatedAt.UTC(), d.ExpiresAt.UTC(),
		expectedSourceAssetID, expectedUpdatedAt.UTC())
	if err != nil {
		return fmt.Errorf("metaldogs designs update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ports.ErrDesignConflict
	}
	return nil
}

func assetStorageKey(ctx context.Context, db designQuerier, id string) (string, error) {
	var key sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT storage_key FROM metaldogs_assets WHERE id = $1
	`, id).Scan(&key)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("metaldogs assets storage_key: %w", err)
	}
	if !key.Valid {
		return "", nil
	}
	return key.String, nil
}

type designScanner interface {
	Scan(dest ...any) error
}

func scanDesign(row designScanner) (domain.Design, bool, error) {
	var d domain.Design
	var texts []byte
	var customer, latestJob, approved sql.NullString
	err := row.Scan(
		&d.ID, &d.TemplateID, &d.TemplateVersion, &d.Status, &texts, &d.SourceAssetID, &d.OwnerTokenHash,
		&customer, &d.GenerationAttempts, &latestJob, &approved,
		&d.CreatedAt, &d.UpdatedAt, &d.ExpiresAt,
	)
	if err == sql.ErrNoRows {
		return domain.Design{}, false, nil
	}
	if err != nil {
		return domain.Design{}, false, fmt.Errorf("metaldogs designs scan: %w", err)
	}
	d.Texts = map[string]string{}
	if len(texts) > 0 {
		if err := json.Unmarshal(texts, &d.Texts); err != nil {
			return domain.Design{}, false, fmt.Errorf("metaldogs designs texts: %w", err)
		}
	}
	if customer.Valid {
		d.CustomerID = customer.String
	}
	if latestJob.Valid {
		d.LatestJobID = latestJob.String
	}
	if approved.Valid {
		d.ApprovedVersionID = approved.String
	}
	d.CreatedAt = d.CreatedAt.UTC()
	d.UpdatedAt = d.UpdatedAt.UTC()
	d.ExpiresAt = d.ExpiresAt.UTC()
	return d, true, nil
}
