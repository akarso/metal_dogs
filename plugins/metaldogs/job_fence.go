package metaldogs

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
)

// InstallGenerateJobFence registers the generate claim fence. The wrap keeps
// jobs.Reader and jobs.Admin (including CountsByStatus) and UnwrapQueue so
// BindQueue accepts it. OnRequeued is Host.OnRequeued(ctx, jobID, attempt):
// the platform runs it from RequeueWithBeforeCommit before the pending
// update, and pluginsdk.RequeueTx is that same *sql.Tx.
func InstallGenerateJobFence(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("metaldogs: install generate job fence: host required")
	}
	h.OnQueueBound(wrapGenerateJobFence)
	h.OnRequeued(func(ctx context.Context, jobID string, _ int) error {
		tx, ok := pluginsdk.RequeueTx(ctx)
		if !ok || tx == nil {
			return fmt.Errorf("metaldogs generate fence: requeue transaction missing")
		}
		return postgres.InvalidateClaimExec(ctx, tx, jobID)
	})
	return nil
}
