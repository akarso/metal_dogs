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

// InstallGenerateJobFence registers the generate claim fence. The wrap keeps
// jobs.Reader and jobs.Admin (including CountsByStatus) and UnwrapQueue so
// BindQueue accepts it. OnRequeued is Host.OnRequeued(ctx, jobID, attempt):
// the platform runs it from RequeueWithBeforeCommit before the pending
// update, and pluginsdk.RequeueTx is that same *sql.Tx.
// The SDK hook has no job type. A missing reclaim transaction fails the
// requeue only when jobID is a metaldogs_generation_jobs row.
func InstallGenerateJobFence(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("metaldogs: install generate job fence: host required")
	}
	var db *sql.DB
	if opened, ok := h.BootstrapDB(); ok {
		db = opened
	}
	h.OnQueueBound(wrapGenerateJobFence)
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
	if db == nil || id == "" {
		return false, nil
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
