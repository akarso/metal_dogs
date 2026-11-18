package metaldogs

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

// generateFenceQueue forwards the bound queue. RequeueHookable is required so
// WrapRequeueNotify keeps OnRequeued inside the reclaim transaction; a plain
// Requeue forward would run the hook after commit.
type generateFenceQueue struct {
	next jobs.Queue
}

func wrapGenerateJobFence(queue any) any {
	next, ok := queue.(jobs.Queue)
	if !ok || next == nil {
		return nil
	}
	return &generateFenceQueue{next: next}
}

func (q *generateFenceQueue) UnwrapQueue() jobs.Queue {
	if q == nil {
		return nil
	}
	return q.next
}

func (q *generateFenceQueue) Enqueue(ctx context.Context, job jobs.Job) error {
	return q.next.Enqueue(ctx, job)
}
func (q *generateFenceQueue) Dequeue(ctx context.Context) (*jobs.Job, error) {
	return q.next.Dequeue(ctx)
}
func (q *generateFenceQueue) Complete(ctx context.Context, id string, attempt int) error {
	return q.next.Complete(ctx, id, attempt)
}
func (q *generateFenceQueue) Fail(ctx context.Context, id string, attempt int, jobErr error) error {
	return q.next.Fail(ctx, id, attempt, jobErr)
}
func (q *generateFenceQueue) Requeue(ctx context.Context, id string, attempt int, opts ...jobs.RequeueOption) error {
	return q.next.Requeue(ctx, id, attempt, opts...)
}

func (q *generateFenceQueue) RequeueWithBeforeCommit(ctx context.Context, id string, attempt int, before jobs.RequeueBeforeCommit, opts ...jobs.RequeueOption) error {
	h, ok := q.next.(jobs.RequeueHookable)
	if !ok {
		return fmt.Errorf("metaldogs generate fence: queue cannot run OnRequeued in the requeue transaction")
	}
	return h.RequeueWithBeforeCommit(ctx, id, attempt, before, opts...)
}

func (q *generateFenceQueue) reader() (jobs.Reader, error) {
	r, ok := q.next.(jobs.Reader)
	if !ok {
		return nil, fmt.Errorf("metaldogs generate fence: queue does not implement jobs.Reader")
	}
	return r, nil
}

func (q *generateFenceQueue) List(ctx context.Context, filter jobs.ListFilter) ([]jobs.Summary, error) {
	r, err := q.reader()
	if err != nil {
		return nil, err
	}
	return r.List(ctx, filter)
}
func (q *generateFenceQueue) Get(ctx context.Context, id string) (*jobs.Detail, error) {
	r, err := q.reader()
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}
func (q *generateFenceQueue) CountsByStatus(ctx context.Context) (map[jobs.Status]int, error) {
	r, err := q.reader()
	if err != nil {
		return nil, err
	}
	return r.CountsByStatus(ctx)
}
func (q *generateFenceQueue) CountsByType(ctx context.Context, jobType string) (map[jobs.Status]int, error) {
	r, err := q.reader()
	if err != nil {
		return nil, err
	}
	return r.CountsByType(ctx, jobType)
}

func (q *generateFenceQueue) admin() (jobs.Admin, error) {
	a, ok := q.next.(jobs.Admin)
	if !ok {
		return nil, fmt.Errorf("metaldogs generate fence: queue does not implement jobs.Admin")
	}
	return a, nil
}

func (q *generateFenceQueue) Retry(ctx context.Context, id string) error {
	a, err := q.admin()
	if err != nil {
		return err
	}
	return a.Retry(ctx, id)
}
func (q *generateFenceQueue) Cancel(ctx context.Context, id string) error {
	a, err := q.admin()
	if err != nil {
		return err
	}
	return a.Cancel(ctx, id)
}

var (
	_ jobs.Queue           = (*generateFenceQueue)(nil)
	_ jobs.Reader          = (*generateFenceQueue)(nil)
	_ jobs.Admin           = (*generateFenceQueue)(nil)
	_ jobs.RequeueHookable = (*generateFenceQueue)(nil)
)
