package metaldogs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/internal/platform/requestctx"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/metaldogs"
)

type memQueue struct {
	mu       sync.Mutex
	enqueued []jobs.Job
}

func (q *memQueue) Enqueue(_ context.Context, job jobs.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.enqueued = append(q.enqueued, job)
	return nil
}
func (q *memQueue) Dequeue(context.Context) (*jobs.Job, error) { return nil, nil }
func (q *memQueue) Complete(context.Context, string) error     { return nil }
func (q *memQueue) Fail(context.Context, string, error) error  { return nil }
func (q *memQueue) snapshot() []jobs.Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]jobs.Job, len(q.enqueued))
	copy(out, q.enqueued)
	return out
}

func TestObsPing_EnqueuePropagatesRequestID(t *testing.T) {
	var logBuf bytes.Buffer
	app := testApp(true)
	app.Logger = logger.NewWithWriter(&logBuf, "info")
	queue := &memQueue{}
	if err := app.BindQueue(queue); err != nil {
		t.Fatal(err)
	}
	if err := metaldogs.New().Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	ctx := requestctx.WithRequestID(context.Background(), "corr-42")
	if err := app.Integration(metaldogs.PluginName).Enqueue(ctx, "obs.ping", map[string]interface{}{"k": 1}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	got := queue.snapshot()
	if len(got) != 1 {
		t.Fatalf("enqueued = %d", len(got))
	}
	if got[0].Type != "integration.sync.metaldogs.obs.ping" {
		t.Fatalf("type = %q", got[0].Type)
	}
	inner, _ := got[0].Payload["payload"].(map[string]interface{})
	if inner["request_id"] != "corr-42" {
		t.Fatalf("payload = %#v", inner)
	}

	handler := findObsHandler(t, app)
	if err := handler(context.Background(), extapi.SyncJobContext{
		JobID:    "job-1",
		Name:     "obs.ping",
		Attempts: 1,
		Payload:  inner,
		Logger:   app.Logger,
	}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	out := logBuf.String()
	if !strings.Contains(out, `"correlation_id":"corr-42"`) || !strings.Contains(out, `"request_id":"corr-42"`) {
		t.Fatalf("log missing correlation fields: %s", out)
	}
	if !strings.Contains(out, `"attempt":1`) {
		t.Fatalf("log missing attempt: %s", out)
	}
}

func findObsHandler(t *testing.T, app *plugin.App) extapi.SyncJobHandler {
	t.Helper()
	for _, reg := range app.SyncJobs() {
		if reg.Job.Name == "obs.ping" {
			return reg.Job.Handler
		}
	}
	t.Fatal("obs.ping not registered")
	return nil
}

func TestPlugin_Init_RegistersHealthAndObsPing(t *testing.T) {
	app := testApp(true)
	app.Logger = logger.NewWithWriter(io.Discard, "error")
	if err := metaldogs.New().Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	routes := app.PublicRoutes()
	patterns := map[string]bool{}
	for _, r := range routes {
		patterns[r.Pattern] = true
	}
	if !patterns["GET /api/customizer/enabled"] || !patterns["GET /api/customizer/health"] {
		t.Fatalf("routes = %+v", routes)
	}
	found := false
	for _, reg := range app.SyncJobs() {
		if reg.JobType == "integration.sync.metaldogs.obs.ping" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SyncJobs = %+v", app.SyncJobs())
	}
	var health plugin.PublicRoute
	for _, r := range routes {
		if r.Pattern == "GET /api/customizer/health" {
			health = r
		}
	}
	rec := httptest.NewRecorder()
	health.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/health", nil))
	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if body["status"] != "degraded" {
		t.Fatalf("health without queue = %#v", body)
	}
}
