package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// MetaStore is a Postgres adapter for ports.MetaStore.
type MetaStore struct {
	db *sql.DB
}

// NewMetaStore returns a MetaStore backed by db.
func NewMetaStore(db *sql.DB) *MetaStore {
	return &MetaStore{db: db}
}

var _ ports.MetaStore = (*MetaStore)(nil)

// Put upserts a meta row.
func (s *MetaStore) Put(ctx context.Context, key, value string) error {
	if key == "" {
		return fmt.Errorf("metaldogs meta: key required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO metaldogs_meta (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value, updated_at = now()
	`, key, value)
	if err != nil {
		return fmt.Errorf("metaldogs meta put: %w", err)
	}
	return nil
}

// Get returns the value for key.
func (s *MetaStore) Get(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM metaldogs_meta WHERE key = $1`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("metaldogs meta get: %w", err)
	}
	return value, true, nil
}
