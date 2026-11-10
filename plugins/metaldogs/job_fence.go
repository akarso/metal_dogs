package metaldogs

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/jobs"
	infraPostgres "github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
)

// generateJobFenceQueue is the Postgres job queue surface needed to install the
// generate claim fence for Retry/Requeue (including jobs:retry and admin retry).
type generateJobFenceQueue interface {
	SetOnRequeued(fn func(ctx context.Context, exec infraPostgres.RequeueExec, jobID, jobType string) error)
	RequireOnRequeued(jobTypes ...string)
}

// InstallGenerateJobFence registers OnRequeued + RequireOnRequeued so every
// re-pend of integration.sync.metaldogs.generate invalidates the product claim
// inside the same transaction. Safe to call more than once.
func InstallGenerateJobFence(queue jobs.Queue) error {
	if queue == nil {
		return fmt.Errorf("metaldogs: install generate job fence: queue required")
	}
	q, ok := queue.(generateJobFenceQueue)
	if !ok {
		return fmt.Errorf("metaldogs: install generate job fence: queue does not support OnRequeued (require queue.driver=postgres)")
	}
	jobType := infraPostgres.MetalDogsGenerateJobType
	q.RequireOnRequeued(jobType)
	q.SetOnRequeued(func(ctx context.Context, exec infraPostgres.RequeueExec, jobID, jt string) error {
		if jt != jobType {
			return nil
		}
		return postgres.InvalidateClaimExec(ctx, exec, jobID)
	})
	return nil
}
