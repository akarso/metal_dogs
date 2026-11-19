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

type fenceHookStub struct{}

func (f *fenceHookStub) Enqueue(context.Context, jobs.Job) error        { return nil }
func (f *fenceHookStub) Dequeue(context.Context) (*jobs.Job, error)     { return nil, nil }
func (f *fenceHookStub) Complete(context.Context, string, int) error    { return nil }
func (f *fenceHookStub) Fail(context.Context, string, int, error) error { return nil }
func (f *fenceHookStub) Requeue(context.Context, string, int, ...jobs.RequeueOption) error {
	return nil
}
func (f *fenceHookStub) RequeueWithBeforeCommit(ctx context.Context, id string, attempt int, before jobs.RequeueBeforeCommit, _ ...jobs.RequeueOption) error {
	if before == nil {
		return nil
	}
	return before(ctx, id, attempt)
}

func TestInstallGenerateJobFence_DoesNotWrapQueue(t *testing.T) {
	t.Parallel()
	app := &plugin.App{}
	h := pluginsdk.AsHostFor(app, metaldogs.PluginName)
	if err := metaldogs.InstallGenerateJobFence(h); err != nil {
		t.Fatal(err)
	}
	inner := &fenceHookStub{}
	got, err := app.ApplyQueueBoundHooks(inner)
	if err != nil {
		t.Fatal(err)
	}
	if got != inner {
		t.Fatal("fence must not wrap the queue")
	}
	bound := app.WrapRequeueNotify(got)
	err = bound.Requeue(context.Background(), "job-1", 1)
	if err == nil || !strings.Contains(err.Error(), "database required") {
		t.Fatalf("requeue without database: %v", err)
	}
}

func TestInstallGenerateJobFence_RejectsNilHost(t *testing.T) {
	t.Parallel()
	if err := metaldogs.InstallGenerateJobFence(nil); err == nil {
		t.Fatal("want error")
	}
}
