package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// OrderDesignSnapshotRepo is a Postgres adapter for ports.OrderDesignSnapshotRepository.
type OrderDesignSnapshotRepo struct {
	db *sql.DB
}

// NewOrderDesignSnapshotRepo returns a repo backed by db.
func NewOrderDesignSnapshotRepo(db *sql.DB) *OrderDesignSnapshotRepo {
	return &OrderDesignSnapshotRepo{db: db}
}

var _ ports.OrderDesignSnapshotRepository = (*OrderDesignSnapshotRepo)(nil)

const orderDesignSnapshotColumns = `
	order_id, variant_id, design_id, design_version_id, sku,
	template_slug, template_version, texts, provider, model, prompt_version,
	artwork_asset_id, artwork_checksum, production_asset_id, production_checksum,
	created_at
`

func (r *OrderDesignSnapshotRepo) Insert(ctx context.Context, s domain.OrderDesignSnapshot) (bool, error) {
	if err := s.Validate(); err != nil {
		return false, err
	}
	texts, err := json.Marshal(s.Texts)
	if err != nil {
		return false, err
	}
	if s.Texts == nil {
		texts = []byte("{}")
	}
	var prodAsset, prodChecksum any
	if s.ProductionAssetID != "" {
		prodAsset = s.ProductionAssetID
		prodChecksum = s.ProductionChecksum
	}
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO metaldogs_order_design_snapshots (
			order_id, variant_id, design_id, design_version_id, sku,
			template_slug, template_version, texts, provider, model, prompt_version,
			artwork_asset_id, artwork_checksum, production_asset_id, production_checksum,
			created_at
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,$8::jsonb,$9,$10,$11,
			$12,$13,$14,$15,
			$16
		)
		ON CONFLICT (order_id, variant_id) DO NOTHING
	`, s.OrderID, s.VariantID, s.DesignID, s.DesignVersionID, s.SKU,
		s.TemplateSlug, s.TemplateVersion, texts, s.Provider, s.Model, s.PromptVersion,
		s.ArtworkAssetID, s.ArtworkChecksum, prodAsset, prodChecksum,
		s.CreatedAt.UTC(),
	)
	if err != nil {
		return false, fmt.Errorf("metaldogs order_design_snapshots insert: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (r *OrderDesignSnapshotRepo) Get(ctx context.Context, orderID, variantID string) (domain.OrderDesignSnapshot, bool, error) {
	if orderID == "" || variantID == "" {
		return domain.OrderDesignSnapshot{}, false, nil
	}
	if err := requireUUIDOrderID(orderID); err != nil {
		return domain.OrderDesignSnapshot{}, false, err
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT `+orderDesignSnapshotColumns+`
		FROM metaldogs_order_design_snapshots
		WHERE order_id = $1 AND variant_id = $2
	`, orderID, variantID)
	return scanOrderDesignSnapshot(row)
}

func (r *OrderDesignSnapshotRepo) ListByOrder(ctx context.Context, orderID string) ([]domain.OrderDesignSnapshot, error) {
	if orderID == "" {
		return nil, nil
	}
	if err := requireUUIDOrderID(orderID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+orderDesignSnapshotColumns+`
		FROM metaldogs_order_design_snapshots
		WHERE order_id = $1
		ORDER BY variant_id
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("metaldogs order_design_snapshots list: %w", err)
	}
	defer rows.Close()
	var out []domain.OrderDesignSnapshot
	for rows.Next() {
		s, ok, err := scanOrderDesignSnapshot(rows)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metaldogs order_design_snapshots list: %w", err)
	}
	return out, nil
}

func (r *OrderDesignSnapshotRepo) FillProduction(ctx context.Context, orderID, variantID, assetID, checksum string) error {
	got, ok, err := r.Get(ctx, orderID, variantID)
	if err != nil {
		return err
	}
	if !ok {
		return ports.ErrSnapshotNotFound
	}
	next, err := got.FillProduction(assetID, checksum)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE metaldogs_order_design_snapshots
		SET production_asset_id = $3, production_checksum = $4
		WHERE order_id = $1 AND variant_id = $2
		  AND production_asset_id IS NULL
		  AND production_checksum IS NULL
	`, orderID, variantID, next.ProductionAssetID, next.ProductionChecksum)
	if err != nil {
		return fmt.Errorf("metaldogs order_design_snapshots fill production: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	return domain.ErrProductionAlreadySet
}

func requireUUIDOrderID(orderID string) error {
	parsed, err := uuid.Parse(orderID)
	if err != nil || parsed.String() != strings.ToLower(orderID) {
		return ports.ErrInvalidOrderID
	}
	return nil
}

func scanOrderDesignSnapshot(row designScanner) (domain.OrderDesignSnapshot, bool, error) {
	var s domain.OrderDesignSnapshot
	var texts []byte
	var prodAsset, prodChecksum sql.NullString
	err := row.Scan(
		&s.OrderID, &s.VariantID, &s.DesignID, &s.DesignVersionID, &s.SKU,
		&s.TemplateSlug, &s.TemplateVersion, &texts, &s.Provider, &s.Model, &s.PromptVersion,
		&s.ArtworkAssetID, &s.ArtworkChecksum, &prodAsset, &prodChecksum,
		&s.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return domain.OrderDesignSnapshot{}, false, nil
	}
	if err != nil {
		return domain.OrderDesignSnapshot{}, false, fmt.Errorf("metaldogs order_design_snapshots scan: %w", err)
	}
	s.Texts = map[string]string{}
	if len(texts) > 0 {
		if err := json.Unmarshal(texts, &s.Texts); err != nil {
			return domain.OrderDesignSnapshot{}, false, fmt.Errorf("metaldogs order_design_snapshots texts: %w", err)
		}
	}
	if prodAsset.Valid {
		s.ProductionAssetID = prodAsset.String
	}
	if prodChecksum.Valid {
		s.ProductionChecksum = prodChecksum.String
	}
	s.CreatedAt = s.CreatedAt.UTC()
	return s, true, nil
}
