package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

var _ ports.ArtworkMockupLookup = (*GenerationJobRepo)(nil)

// MockupAssetForArtwork returns the mockup of the generation job that stored artworkAssetID.
// When no job has that asset, it returns the design's latest mockup whose artwork checksum matches.
func (r *GenerationJobRepo) MockupAssetForArtwork(ctx context.Context, designID, artworkAssetID, artworkChecksum string) (string, bool, error) {
	if r == nil || r.db == nil {
		return "", false, fmt.Errorf("metaldogs generation mockup lookup: db required")
	}
	if _, err := uuid.Parse(strings.TrimSpace(designID)); err != nil {
		return "", false, nil
	}
	if id, err := uuid.Parse(strings.TrimSpace(artworkAssetID)); err == nil {
		mockupID, ok, err := r.latestMockup(ctx, `
			SELECT mockup_asset_id::text
			FROM metaldogs_generation_jobs
			WHERE design_id = $1::uuid
			  AND artwork_asset_id = $2::uuid
			  AND mockup_asset_id IS NOT NULL
			ORDER BY finished_at DESC NULLS LAST, queued_at DESC
			LIMIT 1
		`, designID, id.String())
		if err != nil || ok {
			return mockupID, ok, err
		}
	}
	artworkChecksum = strings.TrimSpace(artworkChecksum)
	if artworkChecksum == "" {
		return "", false, nil
	}
	return r.latestMockup(ctx, `
		SELECT j.mockup_asset_id::text
		FROM metaldogs_generation_jobs j
		JOIN metaldogs_assets a ON a.id = j.artwork_asset_id
		WHERE j.design_id = $1::uuid
		  AND a.checksum = $2
		  AND j.mockup_asset_id IS NOT NULL
		ORDER BY j.finished_at DESC NULLS LAST, j.queued_at DESC
		LIMIT 1
	`, designID, artworkChecksum)
}

func (r *GenerationJobRepo) latestMockup(ctx context.Context, query string, args ...any) (string, bool, error) {
	var id sql.NullString
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&id)
	if err == sql.ErrNoRows || (err == nil && !id.Valid) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("metaldogs generation mockup lookup: %w", err)
	}
	return id.String, id.String != "", nil
}
