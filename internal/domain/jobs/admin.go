package jobs

import (
	"context"
	"time"
)

// Admin is a write-only port for admin-triggered job lifecycle actions —
// separate from Queue (the worker's own dequeue/complete/fail lifecycle)
// and Reader (read-only introspection) because retry/cancel are operator
// corrections, not something the worker does on its own. Kept
// Postgres-queue-only for the same reason Reader is: a broker-backed Queue
// implementation has no queryable/updatable job table the way the
// Postgres implementation does.
type Admin interface {
	// Retry resets a failed job back to pending (attempts reset to 0,
	// run_at set to now) so the worker picks it up again. Also reclaims a
	// processing job whose updated_at is older than DefaultStaleProcessingLease
	// (attempts preserved). Returns a *apperror.Error with CodeNotFound if no
	// job with that ID exists, or CodeConflict if the job exists but isn't
	// failed or stale-processing.
	Retry(ctx context.Context, id string) error

	// Requeue returns a job to pending for another delivery without resetting
	// attempts. requeued is true only when the row was moved to pending.
	// Pending and young processing leave the row alone (requeued=false, err=nil).
	// Allowed from failed, done, cancelled, or processing older than lease.
	// Returns CodeNotFound when missing. Implementations that fence product
	// state must do so in the same transaction as the pending update; a fence
	// failure must fail the Requeue (row stays unrepended).
	Requeue(ctx context.Context, id string, lease time.Duration) (requeued bool, err error)

	// Cancel marks a pending job cancelled — it will never be dequeued.
	// There is no in-flight cancellation: a processing job cannot be
	// cancelled. Returns a *apperror.Error with CodeNotFound if no job
	// with that ID exists, or CodeConflict if the job exists but isn't
	// currently pending.
	Cancel(ctx context.Context, id string) error
}
