package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// AssetRepo is a Postgres adapter for ports.AssetRepository (metadata, not blobs).
type AssetRepo struct {
	db *sql.DB
}

// NewAssetRepo returns an AssetRepo backed by db.
func NewAssetRepo(db *sql.DB) *AssetRepo {
	return &AssetRepo{db: db}
}

var _ ports.AssetRepository = (*AssetRepo)(nil)

const assetColumns = `
	id, type, storage_key, mime_type, size_bytes, width, height, checksum, owner_ref,
	created_at, delete_after, deleted_at
`

// Insert persists a new asset row.
func (s *AssetRepo) Insert(ctx context.Context, a domain.Asset) error {
	if a.ID == "" || a.Type == "" || a.StorageKey == "" || a.MimeType == "" || a.Checksum == "" {
		return fmt.Errorf("metaldogs assets: id, type, storage_key, mime_type, checksum required")
	}
	if a.SizeBytes < 0 || a.Width <= 0 || a.Height <= 0 || a.DeleteAfter.IsZero() {
		return fmt.Errorf("metaldogs assets: size_bytes, width, height, delete_after required")
	}
	var owner any
	if a.OwnerRef != "" {
		owner = a.OwnerRef
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO metaldogs_assets (
			id, type, storage_key, mime_type, size_bytes, width, height, checksum, owner_ref,
			created_at, delete_after, deleted_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,
			COALESCE($10, now()), $11, NULL
		)
	`, a.ID, a.Type, a.StorageKey, a.MimeType, a.SizeBytes, a.Width, a.Height, a.Checksum, owner,
		nullTime(a.CreatedAt), a.DeleteAfter.UTC(),
	)
	if err != nil {
		return fmt.Errorf("metaldogs assets insert: %w", err)
	}
	return nil
}

// GetByID returns an active (non-soft-deleted) asset.
func (s *AssetRepo) GetByID(ctx context.Context, id string) (domain.Asset, bool, error) {
	if id == "" {
		return domain.Asset{}, false, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT `+assetColumns+`
		FROM metaldogs_assets
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	return scanAsset(row)
}

// SoftDelete marks the asset deleted_at (idempotent if already deleted).
func (s *AssetRepo) SoftDelete(ctx context.Context, id string, at time.Time) error {
	if id == "" {
		return fmt.Errorf("metaldogs assets: id required")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE metaldogs_assets SET deleted_at = $2
		WHERE id = $1 AND deleted_at IS NULL
	`, id, at.UTC())
	if err != nil {
		return fmt.Errorf("metaldogs assets soft delete: %w", err)
	}
	return nil
}

// BindOwnerRef sets owner_ref on an active unbound source asset.
func (s *AssetRepo) BindOwnerRef(ctx context.Context, assetID, ownerRef string) error {
	return bindOwnerRef(ctx, s.db, assetID, ownerRef)
}

// ClearOwnerRef clears owner_ref when it matches.
func (s *AssetRepo) ClearOwnerRef(ctx context.Context, assetID, ownerRef string) error {
	return clearOwnerRef(ctx, s.db, assetID, ownerRef)
}

type execContexter interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func bindOwnerRef(ctx context.Context, db execContexter, assetID, ownerRef string) error {
	if assetID == "" || ownerRef == "" {
		return fmt.Errorf("metaldogs assets: asset id and owner_ref required")
	}
	res, err := db.ExecContext(ctx, `
		UPDATE metaldogs_assets
		SET owner_ref = $2
		WHERE id = $1
		  AND deleted_at IS NULL
		  AND type = $3
		  AND owner_ref IS NULL
	`, assetID, ownerRef, domain.AssetTypeSource)
	if err != nil {
		return fmt.Errorf("metaldogs assets bind: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("metaldogs assets bind rows: %w", err)
	}
	if n != 1 {
		return ports.ErrAssetBindConflict
	}
	return nil
}

func clearOwnerRef(ctx context.Context, db execContexter, assetID, ownerRef string) error {
	if assetID == "" || ownerRef == "" {
		return fmt.Errorf("metaldogs assets: asset id and owner_ref required")
	}
	_, err := db.ExecContext(ctx, `
		UPDATE metaldogs_assets
		SET owner_ref = NULL
		WHERE id = $1 AND owner_ref = $2 AND deleted_at IS NULL
	`, assetID, ownerRef)
	if err != nil {
		return fmt.Errorf("metaldogs assets clear owner_ref: %w", err)
	}
	return nil
}

type assetScanner interface {
	Scan(dest ...any) error
}

func scanAsset(row assetScanner) (domain.Asset, bool, error) {
	var a domain.Asset
	var owner sql.NullString
	var deletedAt sql.NullTime
	err := row.Scan(
		&a.ID, &a.Type, &a.StorageKey, &a.MimeType, &a.SizeBytes, &a.Width, &a.Height, &a.Checksum, &owner,
		&a.CreatedAt, &a.DeleteAfter, &deletedAt,
	)
	if err == sql.ErrNoRows {
		return domain.Asset{}, false, nil
	}
	if err != nil {
		return domain.Asset{}, false, fmt.Errorf("metaldogs assets scan: %w", err)
	}
	if owner.Valid {
		a.OwnerRef = owner.String
	}
	if deletedAt.Valid {
		t := deletedAt.Time.UTC()
		a.DeletedAt = &t
	}
	a.CreatedAt = a.CreatedAt.UTC()
	a.DeleteAfter = a.DeleteAfter.UTC()
	return a, true, nil
}
