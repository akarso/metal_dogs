package metaldogs

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

func TestPrepareProductionHandler_PendingSpecFails(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	app := &plugin.App{Logger: logger.NewWithWriter(&buf, "info")}
	h := prepareProductionHandler(app)
	err := h(context.Background(), extapi.SyncJobContext{
		Payload: map[string]interface{}{
			"design_version_id": "ver-1",
			"design_id":         "des-1",
		},
	})
	if err == nil {
		t.Fatal("stub must not ack success")
	}
	out := buf.String()
	if !strings.Contains(out, "metaldogs.prepare_production_artwork.pending_spec") {
		t.Fatalf("log: %s", out)
	}
}

func TestPrepareProductionHandler_MissingPayloadFails(t *testing.T) {
	t.Parallel()
	h := prepareProductionHandler(&plugin.App{Logger: logger.NewWithWriter(ioDiscard{}, "error")})
	if err := h(context.Background(), extapi.SyncJobContext{Payload: map[string]interface{}{}}); err == nil {
		t.Fatal("missing ids must fail")
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func TestResolvePrepareEnqueueConflict_FailedLeftAlone(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusFailed, attempts: 1, updatedAt: time.Now().Add(-time.Hour)}
	q := &queueWithAdmin{conflictAdmin: admin}
	if err := resolvePrepareEnqueueConflict(&plugin.App{}, q, context.Background(), "ver-1", "integration.sync.metaldogs.prepare_production_artwork"); err != nil {
		t.Fatal(err)
	}
	if admin.retried || admin.requeued || admin.status != jobs.StatusFailed {
		t.Fatalf("must not Retry failed: retried=%v requeued=%v status=%s", admin.retried, admin.requeued, admin.status)
	}
}

func TestResolvePrepareEnqueueConflict_DoneLeftAlone(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusDone, updatedAt: time.Now()}
	q := &queueWithAdmin{conflictAdmin: admin}
	if err := resolvePrepareEnqueueConflict(&plugin.App{}, q, context.Background(), "ver-1", "t"); err != nil {
		t.Fatal(err)
	}
	if admin.retried || admin.requeued || admin.status != jobs.StatusDone {
		t.Fatalf("done must stay done: retried=%v requeued=%v status=%s", admin.retried, admin.requeued, admin.status)
	}
}

func TestResolvePrepareEnqueueConflict_YoungProcessingLeftAlone(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusProcessing, updatedAt: time.Now()}
	q := &queueWithAdmin{conflictAdmin: admin}
	if err := resolvePrepareEnqueueConflict(&plugin.App{}, q, context.Background(), "ver-1", "t"); err != nil {
		t.Fatal(err)
	}
	if admin.retried || admin.requeued {
		t.Fatal("young processing must not reclaim")
	}
}

func TestPrepareReclaimRereadOK_TerminalIsSuccess(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusPending}
	if err := prepareReclaimRereadOK(admin, context.Background(), "ver-1"); err != nil {
		t.Fatal(err)
	}
	for _, st := range []jobs.Status{jobs.StatusFailed, jobs.StatusDone, jobs.StatusProcessing} {
		admin.status = st
		if err := prepareReclaimRereadOK(admin, context.Background(), "ver-1"); err != nil {
			t.Fatalf("status %s: %v", st, err)
		}
	}
	admin.status = jobs.StatusCancelled
	if err := prepareReclaimRereadOK(admin, context.Background(), "ver-1"); err == nil {
		t.Fatal("cancelled after unmatched reclaim must error")
	}
}

func TestPrepareStaleReclaimRereadOK_StillStaleProcessingErrors(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{
		status:    jobs.StatusProcessing,
		updatedAt: time.Now().Add(-domain.DefaultClaimLease - time.Second),
	}
	err := prepareStaleReclaimRereadOK(admin, context.Background(), "ver-1")
	if err == nil || !strings.Contains(err.Error(), "still stale processing") {
		t.Fatalf("want still-stale error, got %v", err)
	}
}

func TestPrepareStaleReclaimRereadOK_YoungProcessingOK(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusProcessing, updatedAt: time.Now()}
	if err := prepareStaleReclaimRereadOK(admin, context.Background(), "ver-1"); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareStaleReclaimRereadOK_FailedOK(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusFailed, attempts: 1}
	if err := prepareStaleReclaimRereadOK(admin, context.Background(), "ver-1"); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareReclaimSQL_AttemptsFence(t *testing.T) {
	t.Parallel()
	if strings.Contains(prepareStaleProcessingSQL, "attempts") {
		t.Fatal("stale processing must not touch attempts (zombie Fail fence)")
	}
	if !strings.Contains(prepareCancelledSQL, "attempts = 0") {
		t.Fatal("cancelled reclaim must reset attempts")
	}
	if !strings.Contains(prepareStaleProcessingSQL, "status = 'processing'") {
		t.Fatal("stale UPDATE must constrain status=processing")
	}
	if !strings.Contains(prepareCancelledSQL, "status = 'cancelled'") {
		t.Fatal("cancelled UPDATE must constrain status=cancelled")
	}
}

func TestReclaimStaleProcessing_RaceToFailedPreservesAttempts(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{
		status:    jobs.StatusProcessing,
		attempts:  1,
		updatedAt: time.Now().Add(-domain.DefaultClaimLease - time.Second),
	}
	exec := &prepareExecStub{
		before: func() {
			admin.mu.Lock()
			admin.status = jobs.StatusFailed
			admin.mu.Unlock()
		},
		n: 0, // WHERE status='processing' matches zero rows after Fail
	}
	if err := reclaimStaleProcessingPrepareJob(exec, admin, context.Background(), "ver-1", "t"); err != nil {
		t.Fatal(err)
	}
	if exec.calls != 1 {
		t.Fatalf("want one UPDATE, got %d", exec.calls)
	}
	if strings.Contains(exec.lastQuery, "attempts") {
		t.Fatalf("UPDATE must not touch attempts: %s", exec.lastQuery)
	}
	if admin.retried || admin.status != jobs.StatusFailed || admin.attempts != 1 {
		t.Fatalf("failed row must stay failed with attempts=1: retried=%v status=%s attempts=%d",
			admin.retried, admin.status, admin.attempts)
	}
}

func TestReclaimStaleProcessing_ZeroRowStillStaleErrors(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{
		status:    jobs.StatusProcessing,
		attempts:  1,
		updatedAt: time.Now().Add(-domain.DefaultClaimLease - time.Second),
	}
	exec := &prepareExecStub{n: 0} // type mismatch / clock skew: UPDATE matched nothing
	err := reclaimStaleProcessingPrepareJob(exec, admin, context.Background(), "ver-1", "t")
	if err == nil || !strings.Contains(err.Error(), "still stale processing") {
		t.Fatalf("want still-stale error, got %v", err)
	}
	if admin.status != jobs.StatusProcessing || admin.attempts != 1 {
		t.Fatalf("row must stay processing/attempts=1: status=%s attempts=%d", admin.status, admin.attempts)
	}
}

func TestReclaimStaleProcessing_SuccessLeavesAttemptsUntouched(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusProcessing, attempts: 1}
	exec := &prepareExecStub{n: 1}
	if err := reclaimStaleProcessingPrepareJob(exec, admin, context.Background(), "ver-1", "t"); err != nil {
		t.Fatal(err)
	}
	if admin.attempts != 1 {
		t.Fatalf("reclaim must not rewind attempts locally; got %d", admin.attempts)
	}
	if len(exec.lastArgs) != 3 {
		t.Fatalf("args: %v", exec.lastArgs)
	}
}

func TestReclaimCancelledPrepareJob_RequiresDB(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusCancelled}
	err := reclaimCancelledPrepareJob(nil, admin, context.Background(), "ver-1", "t")
	if err == nil || !strings.Contains(err.Error(), "Bootstrap.DB") {
		t.Fatalf("want Bootstrap.DB error, got %v", err)
	}
}

func TestReclaimCancelledPrepareJob_ResetsAttempts(t *testing.T) {
	t.Parallel()
	admin := &conflictAdmin{status: jobs.StatusCancelled, attempts: 3}
	exec := &prepareExecStub{
		n: 1,
		after: func() {
			admin.mu.Lock()
			admin.status = jobs.StatusPending
			admin.attempts = 0
			admin.mu.Unlock()
		},
	}
	if err := reclaimCancelledPrepareJob(exec, admin, context.Background(), "ver-1", "t"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exec.lastQuery, "attempts = 0") {
		t.Fatalf("cancelled SQL must reset attempts: %s", exec.lastQuery)
	}
}

func TestPrepareJobStale(t *testing.T) {
	t.Parallel()
	if prepareJobStale(time.Now(), domain.DefaultClaimLease) {
		t.Fatal("fresh must not be stale")
	}
	if !prepareJobStale(time.Now().Add(-domain.DefaultClaimLease-time.Second), domain.DefaultClaimLease) {
		t.Fatal("old must be stale")
	}
}

type prepareExecStub struct {
	before    func()
	after     func()
	n         int64
	calls     int
	lastQuery string
	lastArgs  []any
}

func (s *prepareExecStub) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	s.calls++
	s.lastQuery = query
	s.lastArgs = append([]any(nil), args...)
	if s.before != nil {
		s.before()
	}
	if s.after != nil {
		s.after()
	}
	return prepareResult(s.n), nil
}

type prepareResult int64

func (r prepareResult) LastInsertId() (int64, error) { return 0, nil }
func (r prepareResult) RowsAffected() (int64, error) { return int64(r), nil }
