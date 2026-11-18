package metaldogs_test

import (
	"context"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"
)

type fenceQueueStub struct {
	counts map[jobs.Status]int
}

func (f *fenceQueueStub) Enqueue(context.Context, jobs.Job) error        { return nil }
func (f *fenceQueueStub) Dequeue(context.Context) (*jobs.Job, error)     { return nil, nil }
func (f *fenceQueueStub) Complete(context.Context, string, int) error    { return nil }
func (f *fenceQueueStub) Fail(context.Context, string, int, error) error { return nil }
func (f *fenceQueueStub) Requeue(context.Context, string, int, ...jobs.RequeueOption) error {
	return nil
}
func (f *fenceQueueStub) List(context.Context, jobs.ListFilter) ([]jobs.Summary, error) {
	return nil, nil
}
func (f *fenceQueueStub) Get(context.Context, string) (*jobs.Detail, error) { return nil, nil }
func (f *fenceQueueStub) CountsByStatus(context.Context) (map[jobs.Status]int, error) {
	return f.counts, nil
}
func (f *fenceQueueStub) CountsByType(context.Context, string) (map[jobs.Status]int, error) {
	return nil, nil
}
func (f *fenceQueueStub) Retry(context.Context, string) error  { return nil }
func (f *fenceQueueStub) Cancel(context.Context, string) error { return nil }

func TestInstallGenerateJobFence_KeepsReaderAdminAndUnwrap(t *testing.T) {
	t.Parallel()
	app := &plugin.App{}
	h := pluginsdk.AsHostFor(app, metaldogs.PluginName)
	if err := metaldogs.InstallGenerateJobFence(h); err != nil {
		t.Fatal(err)
	}
	inner := &fenceQueueStub{counts: map[jobs.Status]int{jobs.StatusPending: 3}}
	got, err := app.ApplyQueueBoundHooks(inner)
	if err != nil {
		t.Fatal(err)
	}
	u, ok := got.(interface{ UnwrapQueue() jobs.Queue })
	if !ok || u.UnwrapQueue() != inner {
		t.Fatal("wrap must UnwrapQueue to the bound instance")
	}
	reader, ok := got.(jobs.Reader)
	if !ok {
		t.Fatal("wrap dropped jobs.Reader")
	}
	counts, err := reader.CountsByStatus(context.Background())
	if err != nil || counts[jobs.StatusPending] != 3 {
		t.Fatalf("CountsByStatus=%v err=%v", counts, err)
	}
	if _, ok := got.(jobs.Admin); !ok {
		t.Fatal("wrap dropped jobs.Admin")
	}
	hookable, ok := got.(jobs.RequeueHookable)
	if !ok {
		t.Fatal("wrap must forward RequeueWithBeforeCommit")
	}
	err = hookable.RequeueWithBeforeCommit(context.Background(), "job-1", 1, nil)
	if err == nil || !strings.Contains(err.Error(), "requeue transaction") {
		t.Fatalf("inner without RequeueHookable: %v", err)
	}
}

func TestInstallGenerateJobFence_RejectsNilHost(t *testing.T) {
	t.Parallel()
	if err := metaldogs.InstallGenerateJobFence(nil); err == nil {
		t.Fatal("want error")
	}
}

type fenceHookStub struct {
	fenceQueueStub
}

func (f *fenceHookStub) RequeueWithBeforeCommit(ctx context.Context, id string, attempt int, before jobs.RequeueBeforeCommit, _ ...jobs.RequeueOption) error {
	if before == nil {
		return nil
	}
	return before(ctx, id, attempt)
}

func TestInstallGenerateJobFence_RequeueOutsideTx_OtherJobNotBlocked(t *testing.T) {
	t.Parallel()
	app := &plugin.App{}
	h := pluginsdk.AsHostFor(app, metaldogs.PluginName)
	if err := metaldogs.InstallGenerateJobFence(h); err != nil {
		t.Fatal(err)
	}
	inner := &fenceHookStub{}
	wrapped, err := app.ApplyQueueBoundHooks(inner)
	if err != nil {
		t.Fatal(err)
	}
	bound := app.WrapRequeueNotify(wrapped)
	if err := bound.Requeue(context.Background(), "job-1", 1); err != nil {
		t.Fatalf("non-generation requeue without tx: %v", err)
	}
}
