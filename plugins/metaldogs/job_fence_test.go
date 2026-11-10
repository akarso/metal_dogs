package metaldogs_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	infraPostgres "github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs"
)

type fenceQueueStub struct {
	required []string
	hookSet  bool
}

func (f *fenceQueueStub) Enqueue(context.Context, jobs.Job) error             { return nil }
func (f *fenceQueueStub) Dequeue(context.Context) (*jobs.Job, error)           { return nil, nil }
func (f *fenceQueueStub) Complete(context.Context, string, int) error          { return nil }
func (f *fenceQueueStub) Fail(context.Context, string, int, error) error       { return nil }
func (f *fenceQueueStub) RequireOnRequeued(jobTypes ...string)                 { f.required = append(f.required, jobTypes...) }
func (f *fenceQueueStub) SetOnRequeued(func(context.Context, infraPostgres.RequeueExec, string, string) error) {
	f.hookSet = true
}

func TestInstallGenerateJobFence(t *testing.T) {
	t.Parallel()
	q := &fenceQueueStub{}
	if err := metaldogs.InstallGenerateJobFence(q); err != nil {
		t.Fatal(err)
	}
	if !q.hookSet {
		t.Fatal("expected SetOnRequeued")
	}
	if len(q.required) != 1 || q.required[0] != infraPostgres.MetalDogsGenerateJobType {
		t.Fatalf("required=%v", q.required)
	}
}

func TestInstallGenerateJobFence_RejectsUnsupportedQueue(t *testing.T) {
	t.Parallel()
	err := metaldogs.InstallGenerateJobFence(&struct {
		jobs.Queue
	}{})
	if err == nil {
		t.Fatal("want error")
	}
}
