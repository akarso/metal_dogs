package metaldogs

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type conflictAdmin struct {
	mu        sync.Mutex
	requeued  bool
	retried   bool
	status    jobs.Status
	updatedAt time.Time
	hookCalls int
	attempts  int
	retryHook func(*conflictAdmin) error
}

func (a *conflictAdmin) Retry(_ context.Context, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.retryHook != nil {
		return a.retryHook(a)
	}
	switch a.status {
	case jobs.StatusFailed:
		a.status = jobs.StatusPending
		a.attempts = 0
		a.retried = true
		return nil
	case jobs.StatusProcessing:
		if time.Since(a.updatedAt) < jobs.DefaultStaleProcessingLease {
			return apperror.Conflict("job is currently processing and younger than the stale lease")
		}
		a.status = jobs.StatusPending
		a.retried = true
		return nil
	default:
		return apperror.Conflict(fmt.Sprintf("job is %s, not failed", a.status))
	}
}
func (a *conflictAdmin) Cancel(context.Context, string) error { return nil }
func (a *conflictAdmin) Requeue(_ context.Context, _ string, lease time.Duration) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status == jobs.StatusPending {
		return false, nil
	}
	if a.status == jobs.StatusProcessing && time.Since(a.updatedAt) < lease {
		return false, nil
	}
	a.status = jobs.StatusPending
	a.requeued = true
	a.hookCalls++ // simulates OnRequeued inside Requeue
	return true, nil
}
func (a *conflictAdmin) Get(_ context.Context, id string) (*jobs.Detail, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return &jobs.Detail{Summary: jobs.Summary{
		ID: id, Status: a.status, UpdatedAt: a.updatedAt, Attempts: a.attempts,
	}}, nil
}
func (a *conflictAdmin) List(context.Context, jobs.ListFilter) ([]jobs.Summary, error) {
	return nil, nil
}
func (a *conflictAdmin) CountsByStatus(context.Context) (map[jobs.Status]int, error) {
	return nil, nil
}
func (a *conflictAdmin) CountsByType(context.Context, string) (map[jobs.Status]int, error) {
	return nil, nil
}

type queueWithAdmin struct {
	jobs.Queue
	*conflictAdmin
}

func (q *queueWithAdmin) Enqueue(context.Context, jobs.Job) error { return nil }
func (q *queueWithAdmin) Dequeue(context.Context) (*jobs.Job, error) {
	return nil, nil
}
func (q *queueWithAdmin) Complete(context.Context, string, int) error { return nil }
func (q *queueWithAdmin) Fail(context.Context, string, int, error) error {
	return nil
}

func TestResolveGenerateEnqueueConflict_LiveClaimNoOp(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusProcessing, updatedAt: time.Now()}
	q := &queueWithAdmin{conflictAdmin: admin}
	if err := resolveGenerateEnqueueConflict(q, context.Background(), "job-1"); err != nil {
		t.Fatal(err)
	}
	if admin.requeued || admin.hookCalls != 0 {
		t.Fatalf("young processing must not requeue/fence: requeued=%v hooks=%d", admin.requeued, admin.hookCalls)
	}
}

func TestResolveGenerateEnqueueConflict_ReliesOnRequeueHookOnly(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{
		status:    jobs.StatusProcessing,
		updatedAt: time.Now().Add(-10 * time.Minute),
	}
	q := &queueWithAdmin{conflictAdmin: admin}
	if err := resolveGenerateEnqueueConflict(q, context.Background(), "job-1"); err != nil {
		t.Fatal(err)
	}
	if !admin.requeued {
		t.Fatal("stale processing must requeue")
	}
	// Conflict path must not add a second InvalidateClaim; fencing is OnRequeued once.
	if admin.hookCalls != 1 {
		t.Fatalf("OnRequeued-equivalent calls=%d, want 1", admin.hookCalls)
	}
}
