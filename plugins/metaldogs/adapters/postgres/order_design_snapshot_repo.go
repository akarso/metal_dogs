package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

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

type snapshotExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (r *OrderDesignSnapshotRepo) Insert(ctx context.Context, s domain.OrderDesignSnapshot) (bool, error) {
	return insertOrderDesignSnapshot(ctx, r.db, s)
}

// CommitOrder writes every snapshot and moves approved designs to ordered together.
// A failed insert or status check leaves designs and snapshots unchanged.
func (r *OrderDesignSnapshotRepo) CommitOrder(ctx context.Context, snapshots []domain.OrderDesignSnapshot, at time.Time) ([]bool, error) {
	if len(snapshots) == 0 {
		return nil, nil
	}
	if at.IsZero() {
		return nil, fmt.Errorf("metaldogs order_design_snapshots commit: updated_at required")
	}
	for _, s := range snapshots {
		if err := s.Validate(); err != nil {
			return nil, snapshotDesignErr(s.DesignID, err)
		}
		if err := requireUUIDOrderID(s.OrderID); err != nil {
			return nil, snapshotDesignErr(s.DesignID, err)
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("metaldogs order_design_snapshots begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	ids := designIDs(snapshots)
	sort.Strings(ids)
	at = at.UTC()
	for _, id := range ids {
		var status string
		err := tx.QueryRowContext(ctx, `
			SELECT status FROM metaldogs_designs WHERE id = $1 FOR UPDATE
		`, id).Scan(&status)
		if err == sql.ErrNoRows {
			return nil, snapshotDesignErr(id, fmt.Errorf("metaldogs snapshot: design %s not found", id))
		}
		if err != nil {
			return nil, snapshotDesignErr(id, fmt.Errorf("metaldogs snapshot: lock design %s: %w", id, err))
		}
		switch status {
		case domain.DesignApproved:
			res, err := tx.ExecContext(ctx, `
				UPDATE metaldogs_designs
				SET status = $1, updated_at = $2
				WHERE id = $3 AND status = $4
			`, domain.DesignOrdered, at, id, domain.DesignApproved)
			if err != nil {
				return nil, snapshotDesignErr(id, fmt.Errorf("metaldogs snapshot: order design %s: %w", id, err))
			}
			n, err := res.RowsAffected()
			if err != nil {
				return nil, snapshotDesignErr(id, err)
			}
			if n != 1 {
				return nil, snapshotDesignErr(id, fmt.Errorf("metaldogs snapshot: design %s status changed", id))
			}
		case domain.DesignOrdered:
		default:
			return nil, snapshotDesignErr(id, fmt.Errorf("metaldogs snapshot: design %s status %s", id, status))
		}
	}

	written := make([]bool, len(snapshots))
	for i, s := range snapshots {
		ok, err := insertOrderDesignSnapshot(ctx, tx, s)
		if err != nil {
			return nil, snapshotDesignErr(s.DesignID, err)
		}
		written[i] = ok
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("metaldogs order_design_snapshots commit: %w", err)
	}
	return written, nil
}

func snapshotDesignErr(designID string, err error) error {
	if err == nil {
		return nil
	}
	return &ports.SnapshotDesignError{DesignID: designID, Err: err}
}

func designIDs(snapshots []domain.OrderDesignSnapshot) []string {
	seen := make(map[string]struct{}, len(snapshots))
	ids := make([]string, 0, len(snapshots))
	for _, s := range snapshots {
		if _, ok := seen[s.DesignID]; ok {
			continue
		}
		seen[s.DesignID] = struct{}{}
		ids = append(ids, s.DesignID)
	}
	return ids
}

func insertOrderDesignSnapshot(ctx context.Context, db snapshotExecer, s domain.OrderDesignSnapshot) (bool, error) {
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
	res, err := db.ExecContext(ctx, `
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
