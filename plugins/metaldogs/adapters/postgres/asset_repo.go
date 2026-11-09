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
	id, type, storage_key, mime_type, size_bytes, width, height, checksum,
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
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO metaldogs_assets (
			id, type, storage_key, mime_type, size_bytes, width, height, checksum,
			created_at, delete_after, deleted_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,
			COALESCE($9, now()), $10, NULL
		)
	`, a.ID, a.Type, a.StorageKey, a.MimeType, a.SizeBytes, a.Width, a.Height, a.Checksum,
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

type assetScanner interface {
	Scan(dest ...any) error
}

func scanAsset(row assetScanner) (domain.Asset, bool, error) {
	var a domain.Asset
	var deletedAt sql.NullTime
	err := row.Scan(
		&a.ID, &a.Type, &a.StorageKey, &a.MimeType, &a.SizeBytes, &a.Width, &a.Height, &a.Checksum,
		&a.CreatedAt, &a.DeleteAfter, &deletedAt,
	)
	if err == sql.ErrNoRows {
		return domain.Asset{}, false, nil
	}
	if err != nil {
		return domain.Asset{}, false, fmt.Errorf("metaldogs assets scan: %w", err)
	}
	if deletedAt.Valid {
		t := deletedAt.Time.UTC()
		a.DeletedAt = &t
	}
	a.CreatedAt = a.CreatedAt.UTC()
	a.DeleteAfter = a.DeleteAfter.UTC()
	return a, true, nil
}

// SoftDeleteForTest marks an asset deleted (DSN tests only).
func (s *AssetRepo) SoftDeleteForTest(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE metaldogs_assets SET deleted_at = $2 WHERE id = $1
	`, id, at.UTC())
	return err
}
