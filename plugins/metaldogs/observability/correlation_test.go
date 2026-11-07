package observability_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/platform/requestctx"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/metaldogs/observability"
)

func TestCorrelationID(t *testing.T) {
	t.Parallel()
	if got := observability.CorrelationID(context.Background()); got != "" {
		t.Fatalf("empty ctx = %q", got)
	}
	ctx := requestctx.WithRequestID(context.Background(), "req-1")
	if got := observability.CorrelationID(ctx); got != "req-1" {
		t.Fatalf("CorrelationID = %q", got)
	}
}

func TestJobLogFields_AliasesCorrelationID(t *testing.T) {
	t.Parallel()
	fields := observability.JobLogFields(extapi.SyncJobContext{
		JobID:    "j1",
		Name:     "obs.ping",
		Attempts: 2,
		Payload:  map[string]interface{}{"request_id": "req-abc"},
	})
	if fields["request_id"] != "req-abc" || fields["correlation_id"] != "req-abc" {
		t.Fatalf("fields = %#v", fields)
	}
	if fields["attempt"] != 2 || fields["job_id"] != "j1" {
		t.Fatalf("fields = %#v", fields)
	}
}
