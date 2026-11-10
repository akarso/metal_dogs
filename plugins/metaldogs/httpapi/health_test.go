package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
)

type fakeReader struct {
	byType map[string]map[jobs.Status]int
	errFor map[string]error
}

func (f *fakeReader) List(context.Context, jobs.ListFilter) ([]jobs.Summary, error) {
	return nil, nil
}
func (f *fakeReader) Get(context.Context, string) (*jobs.Detail, error) { return nil, nil }
func (f *fakeReader) CountsByStatus(context.Context) (map[jobs.Status]int, error) {
	return nil, nil
}
func (f *fakeReader) CountsByType(_ context.Context, jobType string) (map[jobs.Status]int, error) {
	if f.errFor != nil {
		if err, ok := f.errFor[jobType]; ok {
			return nil, err
		}
	}
	if f.byType == nil {
		return map[jobs.Status]int{}, nil
	}
	return f.byType[jobType], nil
}

// readerQueueFull implements Queue + Reader.
type readerQueueFull struct {
	fakeReader
}

func (q *readerQueueFull) Enqueue(context.Context, jobs.Job) error    { return nil }
func (q *readerQueueFull) Dequeue(context.Context) (*jobs.Job, error) { return nil, nil }
func (q *readerQueueFull) Complete(context.Context, string, int) error     { return nil }
func (q *readerQueueFull) Fail(context.Context, string, int, error) error  { return nil }

type plainQueue struct{}

func (plainQueue) Enqueue(context.Context, jobs.Job) error    { return nil }
func (plainQueue) Dequeue(context.Context) (*jobs.Job, error) { return nil, nil }
func (plainQueue) Complete(context.Context, string, int) error     { return nil }
func (plainQueue) Fail(context.Context, string, int, error) error  { return nil }

func withObsPing(app *plugin.App) {
	_ = app.Integration("metaldogs").RegisterSyncJob(extapi.SyncJob{
		Name:    "obs.ping",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
}

func TestHealth_DegradedWithoutQueue(t *testing.T) {
	t.Parallel()
	app := &plugin.App{Config: &config.Config{}}
	withObsPing(app)
	rec := httptest.NewRecorder()
	httpapi.Health(app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "degraded" || body["reason"] != "degraded" {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["queues"]; ok {
		t.Fatalf("public health must omit depths: %#v", body)
	}
}

func TestHealth_OK_OmitsDepths(t *testing.T) {
	t.Parallel()
	app := &plugin.App{Config: &config.Config{}}
	withObsPing(app)
	q := &readerQueueFull{fakeReader: fakeReader{byType: map[string]map[jobs.Status]int{
		"integration.sync.metaldogs.obs.ping": {jobs.StatusPending: 2},
	}}}
	if err := app.BindQueue(q); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	httpapi.Health(app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["queues"]; ok {
		t.Fatalf("public health must omit depths: %#v", body)
	}
}

func TestHealth_UsesBoundQueueWithoutRegisterQueue(t *testing.T) {
	t.Parallel()
	app := &plugin.App{Config: &config.Config{}}
	withObsPing(app)
	q := &readerQueueFull{}
	if err := app.BindQueue(q); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	httpapi.Health(app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d — BoundQueue-only wiring must not report degraded", rec.Code)
	}
}

func TestAdminHealth_ReportsDepths(t *testing.T) {
	t.Parallel()
	app := &plugin.App{Config: &config.Config{}}
	withObsPing(app)
	q := &readerQueueFull{fakeReader: fakeReader{byType: map[string]map[jobs.Status]int{
		"integration.sync.metaldogs.obs.ping": {
			jobs.StatusPending:    2,
			jobs.StatusProcessing: 1,
			jobs.StatusFailed:     3,
		},
	}}}
	if err := app.BindQueue(q); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	httpapi.AdminHealth(app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/customizer/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status string `json:"status"`
		Queues []struct {
			Type       string `json:"type"`
			Pending    int    `json:"pending"`
			Processing int    `json:"processing"`
			Failed     int    `json:"failed"`
		} `json:"queues"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || len(body.Queues) != 1 {
		t.Fatalf("body = %+v", body)
	}
	if body.Queues[0].Pending != 2 || body.Queues[0].Processing != 1 || body.Queues[0].Failed != 3 {
		t.Fatalf("queue = %+v", body.Queues[0])
	}
}

func TestAdminHealth_KeepsTypeOnCountsError(t *testing.T) {
	t.Parallel()
	app := &plugin.App{Config: &config.Config{}}
	withObsPing(app)
	q := &readerQueueFull{fakeReader: fakeReader{
		errFor: map[string]error{
			"integration.sync.metaldogs.obs.ping": errors.New("db down"),
		},
	}}
	if err := app.BindQueue(q); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	httpapi.AdminHealth(app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/customizer/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
		Queues []struct {
			Type  string `json:"type"`
			Error string `json:"error"`
		} `json:"queues"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "degraded" || body.Reason != "counts_failed" {
		t.Fatalf("body = %+v", body)
	}
	if len(body.Queues) != 1 || body.Queues[0].Error != "counts_failed" {
		t.Fatalf("queues = %+v, want type retained with error", body.Queues)
	}
}

func TestHealth_DegradedWhenQueueNotReader(t *testing.T) {
	t.Parallel()
	app := &plugin.App{Config: &config.Config{}}
	withObsPing(app)
	if err := app.BindQueue(plainQueue{}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	httpapi.Health(app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}
