package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/jackc/pgx/v5/pgconn"
)

// DesignVersionRepo is a Postgres adapter for ports.DesignVersionRepository.
type DesignVersionRepo struct {
	db *sql.DB
	// OnExtendMiss is optional; role is "source" or "mockup" (ids only).
	OnExtendMiss func(role, assetID string)
}

// NewDesignVersionRepo returns a DesignVersionRepo backed by db.
func NewDesignVersionRepo(db *sql.DB) *DesignVersionRepo {
	return &DesignVersionRepo{db: db}
}

var _ ports.DesignVersionRepository = (*DesignVersionRepo)(nil)

const designVersionColumns = `
	id, design_id, template_id, template_version, texts,
	artwork_asset_id, artwork_checksum, production_asset_id, production_checksum,
	production_status, provider, model, prompt_version, parameters, approved_at
`

func (r *DesignVersionRepo) GetByID(ctx context.Context, id string) (domain.DesignVersion, bool, error) {
	if id == "" {
		return domain.DesignVersion{}, false, nil
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT `+designVersionColumns+`
		FROM metaldogs_design_versions WHERE id = $1
	`, id)
	return scanDesignVersion(row)
}

func (r *DesignVersionRepo) GetByDesignID(ctx context.Context, designID string) (domain.DesignVersion, bool, error) {
	if designID == "" {
		return domain.DesignVersion{}, false, nil
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT `+designVersionColumns+`
		FROM metaldogs_design_versions WHERE design_id = $1
	`, designID)
	return scanDesignVersion(row)
}

func (r *DesignVersionRepo) ApproveBound(ctx context.Context, d domain.Design, v domain.DesignVersion) error {
	if d.ID == "" || v.ID == "" || v.DesignID != d.ID || d.ApprovedVersionID != v.ID {
		return fmt.Errorf("metaldogs approve: design/version id mismatch")
	}
	if v.ArtworkAssetID == "" || v.ArtworkChecksum == "" {
		return fmt.Errorf("metaldogs approve: artwork required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metaldogs approve begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var status string
	err = tx.QueryRowContext(ctx, `
		SELECT status FROM metaldogs_designs WHERE id = $1 FOR UPDATE
	`, d.ID).Scan(&status)
	if err == sql.ErrNoRows {
		return ports.ErrDesignConflict
	}
	if err != nil {
		return fmt.Errorf("metaldogs approve lock design: %w", err)
	}
	if status != domain.DesignPreviewReady {
		return ports.ErrDesignConflict
	}

	var artType, ownerRef, storageKey, checksum string
	var deletedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT type, COALESCE(owner_ref, ''), COALESCE(storage_key, ''), checksum, deleted_at
		FROM metaldogs_assets WHERE id = $1 FOR UPDATE
	`, v.ArtworkAssetID).Scan(&artType, &ownerRef, &storageKey, &checksum, &deletedAt)
	if err == sql.ErrNoRows {
		return fmt.Errorf("metaldogs approve: artwork missing: %w", ports.ErrDesignConflict)
	}
	if err != nil {
		return fmt.Errorf("metaldogs approve lock artwork: %w", err)
	}
	if deletedAt.Valid || artType != domain.AssetTypeArtwork || ownerRef != d.ID ||
		storageKey == "" || checksum != v.ArtworkChecksum {
		return fmt.Errorf("metaldogs approve: artwork row invalid: %w", ports.ErrDesignConflict)
	}

	if err := insertDesignVersion(ctx, tx, v); err != nil {
		if isUniqueViolation(err) {
			return ports.ErrDesignConflict
		}
		return err
	}

	// Keep frozen artwork (and source/mockup previews) at least until design expires.
	// Never shorten an existing delete_after (generate may already have ~30d).
	keepUntil := d.ExpiresAt.UTC()
	res, err := tx.ExecContext(ctx, `
		UPDATE metaldogs_designs
		SET status = $2, approved_version_id = $3, updated_at = $4, expires_at = $5
		WHERE id = $1 AND status = 'preview_ready'
	`, d.ID, domain.DesignApproved, v.ID, d.UpdatedAt.UTC(), keepUntil)
	if err != nil {
		return fmt.Errorf("metaldogs designs approve: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ports.ErrDesignConflict
	}
	if n, err := extendAssetDeleteAfter(ctx, tx, d.SourceAssetID, keepUntil); err != nil {
		return err
	} else if n == 0 && d.SourceAssetID != "" {
		if r.OnExtendMiss != nil {
			r.OnExtendMiss("source", d.SourceAssetID)
		}
	}
	nArt, err := extendAssetDeleteAfter(ctx, tx, v.ArtworkAssetID, keepUntil)
	if err != nil {
		return err
	}
	if nArt != 1 {
		return fmt.Errorf("metaldogs approve: artwork delete_after extend matched %d rows", nArt)
	}
	if d.LatestJobID != "" {
		var mockupID sql.NullString
		if err := tx.QueryRowContext(ctx, `
			SELECT mockup_asset_id FROM metaldogs_generation_jobs
			WHERE id = $1 AND design_id = $2
		`, d.LatestJobID, d.ID).Scan(&mockupID); err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("metaldogs approve mockup lookup: %w", err)
		}
		if mockupID.Valid && mockupID.String != "" {
			if n, err := extendAssetDeleteAfter(ctx, tx, mockupID.String, keepUntil); err != nil {
				return err
			} else if n == 0 && r.OnExtendMiss != nil {
				r.OnExtendMiss("mockup", mockupID.String)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("metaldogs approve commit: %w", err)
	}
	return nil
}

type versionExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertDesignVersion(ctx context.Context, db versionExecer, v domain.DesignVersion) error {
	if v.ID == "" || v.DesignID == "" || v.TemplateID == "" || v.TemplateVersion < 1 ||
		v.ArtworkAssetID == "" || v.ArtworkChecksum == "" || v.ProductionStatus == "" {
		return fmt.Errorf("metaldogs design_versions: required fields missing")
	}
	texts, err := json.Marshal(v.Texts)
	if err != nil {
		return err
	}
	params := v.ParametersJSON
	if len(params) == 0 {
		params = []byte("{}")
	}
	var prodAsset, prodChecksum any
	if v.ProductionAssetID != "" {
		prodAsset = v.ProductionAssetID
	}
	if v.ProductionChecksum != "" {
		prodChecksum = v.ProductionChecksum
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO metaldogs_design_versions (
			id, design_id, template_id, template_version, texts,
			artwork_asset_id, artwork_checksum, production_asset_id, production_checksum,
			production_status, provider, model, prompt_version, parameters, approved_at
		) VALUES (
			$1,$2,$3,$4,$5::jsonb,
			$6,$7,$8,$9,
			$10,$11,$12,$13,$14::jsonb,$15
		)
	`, v.ID, v.DesignID, v.TemplateID, v.TemplateVersion, texts,
		v.ArtworkAssetID, v.ArtworkChecksum, prodAsset, prodChecksum,
		v.ProductionStatus, v.Provider, v.Model, v.PromptVersion, params, v.ApprovedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("metaldogs design_versions insert: %w", err)
	}
	return nil
}

func scanDesignVersion(row designScanner) (domain.DesignVersion, bool, error) {
	var v domain.DesignVersion
	var texts, params []byte
	var prodAsset, prodChecksum sql.NullString
	err := row.Scan(
		&v.ID, &v.DesignID, &v.TemplateID, &v.TemplateVersion, &texts,
		&v.ArtworkAssetID, &v.ArtworkChecksum, &prodAsset, &prodChecksum,
		&v.ProductionStatus, &v.Provider, &v.Model, &v.PromptVersion, &params, &v.ApprovedAt,
	)
	if err == sql.ErrNoRows {
		return domain.DesignVersion{}, false, nil
	}
	if err != nil {
		return domain.DesignVersion{}, false, fmt.Errorf("metaldogs design_versions scan: %w", err)
	}
	v.Texts = map[string]string{}
	if len(texts) > 0 {
		if err := json.Unmarshal(texts, &v.Texts); err != nil {
			return domain.DesignVersion{}, false, fmt.Errorf("metaldogs design_versions texts: %w", err)
		}
	}
	if prodAsset.Valid {
		v.ProductionAssetID = prodAsset.String
	}
	if prodChecksum.Valid {
		v.ProductionChecksum = prodChecksum.String
	}
	v.ParametersJSON = append([]byte(nil), params...)
	v.ApprovedAt = v.ApprovedAt.UTC()
	return v, true, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
