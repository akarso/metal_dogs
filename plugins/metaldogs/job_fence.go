package metaldogs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
)

// InstallGenerateJobFence registers Host.OnRequeued so the claim bump commits with the re-pend (pluginsdk.RequeueTx).
// A missing transaction fails the requeue for a generation-job row; no database handle fails closed.
func InstallGenerateJobFence(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("metaldogs: install generate job fence: host required")
	}
	var db *sql.DB
	if opened, ok := h.BootstrapDB(); ok {
		db = opened
	}
	h.OnRequeued(func(ctx context.Context, jobID string, _ int) error {
		tx, ok := pluginsdk.RequeueTx(ctx)
		if ok && tx != nil {
			return postgres.InvalidateClaimExec(ctx, tx, jobID)
		}
		owned, err := generationJobRow(ctx, db, jobID)
		if err != nil {
			return err
		}
		if !owned {
			return nil
		}
		return fmt.Errorf("metaldogs generate fence: requeue transaction missing")
	})
	return nil
}

func generationJobRow(ctx context.Context, db *sql.DB, id string) (bool, error) {
	if id == "" {
		return false, nil
	}
	if db == nil {
		return false, fmt.Errorf("metaldogs generate fence: database required")
	}
	var one int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM metaldogs_generation_jobs WHERE id = $1`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		var pgErr *pgconn.PgError
		// 22P02: id is not a uuid, so it is not a generation-job row.
		if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
			return false, nil
		}
		return false, fmt.Errorf("metaldogs generate fence: generation job lookup: %w", err)
	}
	return true, nil
}
