package metaldogs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

type conflictAdmin struct {
	mu          sync.Mutex
	requeued    bool
	retried     bool
	missing     bool
	jobType     string
	status      jobs.Status
	updatedAt   time.Time
	hookCalls   int
	attempts    int
	retryHook   func(*conflictAdmin) error
	beforeCheck func(*conflictAdmin, bool)
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
		if time.Since(a.updatedAt) < domain.DefaultClaimLease {
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
func (a *conflictAdmin) Get(_ context.Context, id string) (*jobs.Detail, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.missing {
		return nil, nil
	}
	return &jobs.Detail{Summary: jobs.Summary{
		ID: id, Type: a.jobType, Status: a.status, UpdatedAt: a.updatedAt, Attempts: a.attempts,
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
	enqueueErr error
}

func (q *queueWithAdmin) Enqueue(context.Context, jobs.Job) error { return q.enqueueErr }
func (q *queueWithAdmin) Dequeue(context.Context) (*jobs.Job, error) {
	return nil, nil
}
func (q *queueWithAdmin) Complete(context.Context, string, int) error { return nil }
func (q *queueWithAdmin) Fail(context.Context, string, int, error) error {
	return nil
}
func (q *queueWithAdmin) Requeue(_ context.Context, _ string, attempt int, opts ...jobs.RequeueOption) error {
	a := q.conflictAdmin
	a.mu.Lock()
	defer a.mu.Unlock()
	settings := jobs.ResolveRequeueOptions(opts)
	if a.beforeCheck != nil {
		a.beforeCheck(a, settings.Terminal)
	}
	if settings.Terminal {
		switch a.status {
		case jobs.StatusFailed, jobs.StatusDone, jobs.StatusCancelled:
		default:
			return jobs.ErrRequeueNotTerminal
		}
	} else {
		if a.status != jobs.StatusProcessing {
			return jobs.ErrRequeueNotProcessing
		}
		if time.Since(a.updatedAt) < settings.MinAge {
			return jobs.ErrRequeueTooFresh
		}
	}
	if attempt != a.attempts {
		return jobs.ErrRequeueWrongAttempt
	}
	a.status = jobs.StatusPending
	a.requeued = true
	a.hookCalls++ // OnRequeued once inside Queue.Requeue
	return nil
}

const generateConflictJobID = "11111111-1111-4111-8111-111111111111"

func conflictEnqueue(admin *conflictAdmin) *queueWithAdmin {
	admin.jobType = "integration.sync.metaldogs.generate"
	return &queueWithAdmin{
		conflictAdmin: admin,
		enqueueErr:    fmt.Errorf("job_queue: enqueue: %w", apperror.Conflict("job already enqueued")),
	}
}

func TestEnqueueGenerate_SDKWrongAttemptReloadsOnce(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusFailed, attempts: 2}
	admin.beforeCheck = func(a *conflictAdmin, terminal bool) {
		if terminal && a.attempts == 2 {
			a.attempts = 3
			a.status = jobs.StatusDone
		}
	}
	q := conflictEnqueue(admin)
	if err := enqueueGenerateOn(t, q, generateConflictJobID); err != nil {
		t.Fatal(err)
	}
	if !admin.requeued || admin.status != jobs.StatusPending || admin.attempts != 3 {
		t.Fatalf("plugin must re-pend once with the live attempt: requeued=%v status=%s attempts=%d", admin.requeued, admin.status, admin.attempts)
	}
	if admin.hookCalls != 1 {
		t.Fatalf("OnRequeued-equivalent calls=%d, want 1", admin.hookCalls)
	}
}

func TestEnqueueGenerate_SDKWrongAttemptStopsAfterOneRetry(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusFailed, attempts: 2}
	admin.beforeCheck = func(a *conflictAdmin, terminal bool) {
		if terminal {
			a.attempts++
			a.status = jobs.StatusDone
		}
	}
	q := conflictEnqueue(admin)
	err := enqueueGenerateOn(t, q, generateConflictJobID)
	if err == nil || !errors.Is(err, jobs.ErrRequeueWrongAttempt) {
		t.Fatalf("second mismatch must surface: %v", err)
	}
	if admin.requeued {
		t.Fatal("must not re-pend after the single retry")
	}
}

func TestEnqueueGenerate_SDKNotTerminalPendingIsSuccess(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusFailed, attempts: 1}
	admin.beforeCheck = func(a *conflictAdmin, terminal bool) {
		if terminal {
			a.status = jobs.StatusPending
		}
	}
	q := conflictEnqueue(admin)
	if err := enqueueGenerateOn(t, q, generateConflictJobID); err != nil {
		t.Fatal(err)
	}
	if admin.requeued || admin.status != jobs.StatusPending {
		t.Fatalf("pending after a lost terminal requeue must stay: requeued=%v status=%s", admin.requeued, admin.status)
	}
}

func TestEnqueueGenerate_SDKNotTerminalReclaimingErrors(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusFailed, attempts: 1}
	admin.beforeCheck = func(a *conflictAdmin, terminal bool) {
		if terminal {
			a.status = jobs.StatusReclaiming
		}
	}
	q := conflictEnqueue(admin)
	err := enqueueGenerateOn(t, q, generateConflictJobID)
	if err == nil || !strings.Contains(err.Error(), string(jobs.StatusReclaiming)) {
		t.Fatalf("reclaiming raced under terminal requeue must error, got %v", err)
	}
	if admin.requeued {
		t.Fatal("reclaiming must not requeue")
	}
}

func TestEnqueueGenerate_ReclaimingAtConflictIsSuccess(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusReclaiming, attempts: 1}
	q := conflictEnqueue(admin)
	if err := enqueueGenerateOn(t, q, generateConflictJobID); err != nil {
		t.Fatal(err)
	}
	if admin.requeued {
		t.Fatal("WithJobID treats reclaiming as in-flight success")
	}
}

func hostJobsFor(t *testing.T, app *plugin.App, q jobs.Queue) ports.JobAccess {
	t.Helper()
	app.RecordNeedsFor(PluginName, pluginsdk.CapJobs)
	if err := app.BindQueue(q); err != nil {
		t.Fatal(err)
	}
	port, err := pluginsdk.NewJobsPort(q.(jobs.Reader), q)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.BindJobsPort(port); err != nil {
		t.Fatal(err)
	}
	return PlatformJobs(pluginsdk.AsHostFor(app, PluginName), nil)
}

func enqueueGenerateOn(t *testing.T, q *queueWithAdmin, id string) error {
	t.Helper()
	app := &plugin.App{}
	if err := registerGenerate(pluginsdk.New(app, PluginName).Integration(PluginName), &application.GenerateWorker{}, 3); err != nil {
		t.Fatal(err)
	}
	return enqueueGenerateIdempotent(hostJobsFor(t, app, q), context.Background(), id, "design-1")
}

func TestEnqueueGenerate_RejectsNonUUIDJobID(t *testing.T) {
	t.Parallel()
	app := &plugin.App{}
	if err := registerGenerate(pluginsdk.New(app, PluginName).Integration(PluginName), &application.GenerateWorker{}, 3); err != nil {
		t.Fatal(err)
	}
	q := &queueWithAdmin{conflictAdmin: &conflictAdmin{}}
	err := enqueueGenerateIdempotent(hostJobsFor(t, app, q), context.Background(), "job-1", "design-1")
	if err == nil || !strings.Contains(err.Error(), "not a UUID") {
		t.Fatalf("non-UUID id: %v", err)
	}
}

func TestEnqueueGenerate_SDKLeavesFreshProcessingConflict(t *testing.T) {
	t.Parallel()
	app := &plugin.App{}
	if err := registerGenerate(pluginsdk.New(app, PluginName).Integration(PluginName), &application.GenerateWorker{}, 3); err != nil {
		t.Fatal(err)
	}
	admin := &conflictAdmin{
		status:    jobs.StatusProcessing,
		updatedAt: time.Now(),
		jobType:   "integration.sync.metaldogs.generate",
	}
	q := &queueWithAdmin{
		conflictAdmin: admin,
		enqueueErr:    fmt.Errorf("job_queue: enqueue: %w", apperror.Conflict("job already enqueued")),
	}
	access := hostJobsFor(t, app, q)
	const id = "11111111-1111-4111-8111-111111111111"
	if err := enqueueGenerateIdempotent(access, context.Background(), id, "design-1"); err != nil {
		t.Fatal(err)
	}
	if admin.requeued {
		t.Fatal("young processing claim must stay")
	}

	q.enqueueErr = fmt.Errorf("job_queue: enqueue: %w", errors.New("connection reset"))
	err := enqueueGenerateIdempotent(access, context.Background(), id, "design-1")
	if err == nil || !strings.Contains(err.Error(), "connection reset") || strings.Contains(err.Error(), "requeue after conflict") {
		t.Fatalf("non-conflict must not enter requeue: %v", err)
	}
}
