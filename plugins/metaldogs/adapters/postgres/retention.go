package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

const (
	// RetentionAssetKeptEvent is the one log line for an asset a deletion path skipped.
	RetentionAssetKeptEvent = "metaldogs.retention.asset_kept"

	RetentionPathAssetCleanup      = "asset_cleanup"
	RetentionPathUnattachedUploads = "unattached_upload_cleanup"
	RetentionPathDesignExpiry      = "design_expiry"
	retentionPathSoftDelete        = "soft_delete"
	retentionPathReplaceSource     = "replace_source"
	retentionPathReplaceLiveAsset  = "replace_live_asset"
)

// dbSession is *sql.DB or *sql.Tx.
type dbSession interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// AssetsReferencedBySnapshots reports which of assetIDs are an order snapshot's
// artwork_asset_id or production_asset_id. It is the only snapshot-protection decision.
func AssetsReferencedBySnapshots(ctx context.Context, q dbSession, assetIDs []string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	ids := canonicalUUIDs(assetIDs)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `
		SELECT artwork_asset_id::text FROM metaldogs_order_design_snapshots
		WHERE artwork_asset_id = ANY($1::uuid[])
		UNION
		SELECT production_asset_id::text FROM metaldogs_order_design_snapshots
		WHERE production_asset_id = ANY($1::uuid[])
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("metaldogs snapshot asset refs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("metaldogs snapshot asset refs scan: %w", err)
		}
		if canon, ok := canonicalUUID(id); ok {
			out[canon] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metaldogs snapshot asset refs rows: %w", err)
	}
	return out, nil
}

// CleanupDueAssets soft-deletes active assets whose delete_after has passed.
// Snapshot-referenced assets stay active.
func CleanupDueAssets(ctx context.Context, db *sql.DB, now time.Time, log pluginsdk.Logger) error {
	return cleanupDue(ctx, db, now, log, RetentionPathAssetCleanup, false)
}

// CleanupUnattachedUploads soft-deletes active assets with no owner_ref whose delete_after has passed.
// Snapshot-referenced assets stay active.
func CleanupUnattachedUploads(ctx context.Context, db *sql.DB, now time.Time, log pluginsdk.Logger) error {
	return cleanupDue(ctx, db, now, log, RetentionPathUnattachedUploads, true)
}

// ExpireDesigns marks designs past expires_at as expired and soft-deletes their assets.
// Ordered designs are left alone. Snapshot-referenced assets stay active.
func ExpireDesigns(ctx context.Context, db *sql.DB, now time.Time, log pluginsdk.Logger) error {
	if db == nil {
		return fmt.Errorf("metaldogs retention: db required")
	}
	if now.IsZero() {
		return fmt.Errorf("metaldogs retention: now required")
	}
	now = now.UTC()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metaldogs retention expire begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	designIDs, err := queryIDs(ctx, tx, `
		SELECT id::text FROM metaldogs_designs
		WHERE expires_at <= $1 AND status IN (`+expirableStatusList()+`)
		ORDER BY id
		FOR UPDATE
	`, now)
	if err != nil {
		return fmt.Errorf("metaldogs retention expire list: %w", err)
	}
	var kept []string
	for _, designID := range designIDs {
		assetIDs, err := designAssetIDs(ctx, tx, designID)
		if err != nil {
			return err
		}
		drop, keep, err := partitionSnapshotAssets(ctx, tx, assetIDs)
		if err != nil {
			return err
		}
		kept = append(kept, keep...)
		if err := markAssetsDeleted(ctx, tx, drop, now); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE metaldogs_designs
			SET status = $2, updated_at = $3
			WHERE id = $1::uuid AND status IN (`+expirableStatusList()+`)
		`, designID, domain.DesignExpired, now)
		if err != nil {
			return fmt.Errorf("metaldogs retention expire design: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("metaldogs retention expire design %s: status changed", designID)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("metaldogs retention expire commit: %w", err)
	}
	logAssetsKept(log, RetentionPathDesignExpiry, kept)
	return nil
}

func cleanupDue(ctx context.Context, db *sql.DB, now time.Time, log pluginsdk.Logger, path string, unattachedOnly bool) error {
	if db == nil {
		return fmt.Errorf("metaldogs retention: db required")
	}
	if now.IsZero() {
		return fmt.Errorf("metaldogs retention: now required")
	}
	now = now.UTC()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metaldogs retention cleanup begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := `
		SELECT id::text FROM metaldogs_assets
		WHERE deleted_at IS NULL AND delete_after <= $1
	`
	if unattachedOnly {
		q += ` AND owner_ref IS NULL`
	}
	q += ` ORDER BY id FOR UPDATE`
	ids, err := queryIDs(ctx, tx, q, now)
	if err != nil {
		return fmt.Errorf("metaldogs retention cleanup list: %w", err)
	}
	drop, keep, err := partitionSnapshotAssets(ctx, tx, ids)
	if err != nil {
		return err
	}
	if err := markAssetsDeleted(ctx, tx, drop, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("metaldogs retention cleanup commit: %w", err)
	}
	logAssetsKept(log, path, keep)
	return nil
}

func expirableStatusList() string {
	statuses := []string{
		domain.DesignDraft,
		domain.DesignGenerating,
		domain.DesignGenerationFailed,
		domain.DesignPreviewReady,
		domain.DesignApproved,
	}
	quoted := make([]string, len(statuses))
	for i, st := range statuses {
		quoted[i] = "'" + st + "'"
	}
	return strings.Join(quoted, ",")
}

func designAssetIDs(ctx context.Context, q dbSession, designID string) ([]string, error) {
	var source string
	err := q.QueryRowContext(ctx, `
		SELECT source_asset_id::text FROM metaldogs_designs WHERE id = $1
	`, designID).Scan(&source)
	if err != nil {
		return nil, fmt.Errorf("metaldogs retention design source: %w", err)
	}
	ids, err := queryIDs(ctx, q, `
		SELECT id::text FROM metaldogs_assets
		WHERE deleted_at IS NULL AND (
			id = $1::uuid
			OR owner_ref = $2
			OR id IN (
				SELECT artwork_asset_id FROM metaldogs_design_versions WHERE design_id = $3::uuid
				UNION
				SELECT production_asset_id FROM metaldogs_design_versions
				WHERE design_id = $3::uuid AND production_asset_id IS NOT NULL
			)
		)
		ORDER BY id
		FOR UPDATE
	`, source, designID, designID)
	if err != nil {
		return nil, fmt.Errorf("metaldogs retention design assets: %w", err)
	}
	if source != "" {
		ids = append(ids, source)
	}
	return ids, nil
}

func partitionSnapshotAssets(ctx context.Context, q dbSession, assetIDs []string) (drop, keep []string, err error) {
	assetIDs = uniqueStrings(assetIDs)
	// FOR UPDATE before the snapshot read so CommitOrder's FOR SHARE cannot commit a reference between the check and deleted_at.
	if err := lockAssetsForUpdate(ctx, q, assetIDs); err != nil {
		return nil, nil, err
	}
	referenced, err := AssetsReferencedBySnapshots(ctx, q, assetIDs)
	if err != nil {
		return nil, nil, err
	}
	for _, id := range assetIDs {
		canon, ok := canonicalUUID(id)
		if ok {
			if _, referenced := referenced[canon]; referenced {
				keep = append(keep, canon)
				continue
			}
		}
		drop = append(drop, id)
	}
	return drop, keep, nil
}

func lockAssetsForUpdate(ctx context.Context, q dbSession, assetIDs []string) error {
	ids := canonicalUUIDs(assetIDs)
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	if _, err := queryIDs(ctx, q, `
		SELECT id::text FROM metaldogs_assets
		WHERE id = ANY($1::uuid[])
		ORDER BY id
		FOR UPDATE
	`, ids); err != nil {
		return fmt.Errorf("metaldogs retention lock assets: %w", err)
	}
	return nil
}

func markAssetsDeleted(ctx context.Context, db dbSession, ids []string, at time.Time) error {
	for _, id := range ids {
		if err := markAssetDeleted(ctx, db, id, at); err != nil {
			return err
		}
	}
	return nil
}

func markAssetDeleted(ctx context.Context, db dbSession, id string, at time.Time) error {
	if id == "" {
		return fmt.Errorf("metaldogs assets: id required")
	}
	_, err := db.ExecContext(ctx, `
		UPDATE metaldogs_assets SET deleted_at = $2
		WHERE id = $1 AND deleted_at IS NULL
	`, id, at.UTC())
	if err != nil {
		return fmt.Errorf("metaldogs assets soft delete: %w", err)
	}
	return nil
}

func logAssetsKept(log pluginsdk.Logger, path string, ids []string) {
	if log == nil {
		return
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		log.Info(RetentionAssetKeptEvent, map[string]interface{}{
			"path":     path,
			"asset_id": id,
		})
	}
}

func queryIDs(ctx context.Context, q dbSession, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func canonicalUUIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	var out []string
	for _, id := range ids {
		canon, ok := canonicalUUID(id)
		if !ok {
			continue
		}
		if _, dup := seen[canon]; dup {
			continue
		}
		seen[canon] = struct{}{}
		out = append(out, canon)
	}
	return out
}

func canonicalUUID(id string) (string, bool) {
	u, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return "", false
	}
	return u.String(), true
}

func uniqueStrings(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	var out []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
