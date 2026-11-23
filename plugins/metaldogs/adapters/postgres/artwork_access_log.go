package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// ArtworkAccessLogRepo appends rows to metaldogs_artwork_access_log.
type ArtworkAccessLogRepo struct {
	db *sql.DB
}

// NewArtworkAccessLogRepo returns a repo backed by db.
func NewArtworkAccessLogRepo(db *sql.DB) *ArtworkAccessLogRepo {
	return &ArtworkAccessLogRepo{db: db}
}

var _ ports.ArtworkAccessLog = (*ArtworkAccessLogRepo)(nil)

// Append writes every event in one transaction. An empty slice writes nothing.
func (r *ArtworkAccessLogRepo) Append(ctx context.Context, events []ports.ArtworkAccessEvent) error {
	if len(events) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("metaldogs artwork access log: database required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metaldogs artwork access log begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, e := range events {
		if err := appendArtworkAccess(ctx, tx, e); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("metaldogs artwork access log commit: %w", err)
	}
	return nil
}

func appendArtworkAccess(ctx context.Context, tx *sql.Tx, e ports.ArtworkAccessEvent) error {
	orderID, err := uuid.Parse(strings.TrimSpace(e.OrderID))
	if err != nil || orderID.String() != strings.ToLower(strings.TrimSpace(e.OrderID)) {
		return fmt.Errorf("metaldogs artwork access log: order id is not a uuid")
	}
	variantID := strings.TrimSpace(e.VariantID)
	actor := strings.TrimSpace(e.Actor)
	if variantID == "" || actor == "" || e.RequestedAt.IsZero() {
		return fmt.Errorf("metaldogs artwork access log: variant, actor, and requested_at required")
	}
	switch e.Kind {
	case ports.ArtworkKindApproved, ports.ArtworkKindProduction:
	default:
		return fmt.Errorf("metaldogs artwork access log: kind must be approved or production")
	}
	switch e.Channel {
	case ports.ArtworkChannelAdmin, ports.ArtworkChannelCLI:
	default:
		return fmt.Errorf("metaldogs artwork access log: channel must be admin or cli")
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO metaldogs_artwork_access_log (
			order_id, variant_id, kind, actor, channel, requested_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, orderID.String(), variantID, e.Kind, actor, e.Channel, e.RequestedAt.UTC())
	if err != nil {
		return fmt.Errorf("metaldogs artwork access log insert: %w", err)
	}
	return nil
}
