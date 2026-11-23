package metaldogs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestEnqueueSnapshotOrder_CanonicalID(t *testing.T) {
	t.Parallel()
	rec := &recordingJobs{}
	p := &Plugin{jobs: rec}
	const lower = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if err := p.EnqueueSnapshotOrder(context.Background(), "  "+strings.ToUpper(lower)+"  "); err != nil {
		t.Fatal(err)
	}
	if err := p.EnqueueSnapshotOrder(context.Background(), lower); err != nil {
		t.Fatal(err)
	}
	if len(rec.ids) != 2 || rec.ids[0] != rec.ids[1] || rec.ids[0] != SnapshotOrderJobID(lower) {
		t.Fatalf("ids %v", rec.ids)
	}
	if rec.payloads[0]["order_id"] != lower || rec.payloads[1]["order_id"] != lower {
		t.Fatalf("payloads %+v", rec.payloads)
	}
	err := p.EnqueueSnapshotOrder(context.Background(), "not-a-uuid")
	if !errors.Is(err, ports.ErrInvalidOrderID) {
		t.Fatalf("%v", err)
	}
	if len(rec.ids) != 2 {
		t.Fatalf("invalid id enqueued %v", rec.ids)
	}
}

func TestLogSnapshotFailure_CommitRollsBackEveryLine(t *testing.T) {
	t.Parallel()
	log := &sliceSyncLog{}
	job := extapi.SyncJobContext{Logger: log}
	err := &application.SnapshotOrderError{
		OrderID:   "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		DesignID:  "d-second",
		VariantID: "v-second",
		Err:       fmt.Errorf("metaldogs snapshot: design d-second status changed"),
		FailedLines: []application.SnapshotLineOutcome{
			{VariantID: "v-first", DesignID: "d-first"},
			{VariantID: "v-second", DesignID: "d-second"},
		},
	}
	logSnapshotFailure(job, err.OrderID, err)
	if len(log.errors) != 2 {
		t.Fatalf("logs %+v", log.errors)
	}
	if !strings.Contains(log.errors[0], "d-first") || !strings.Contains(log.errors[0], "v-first") {
		t.Fatalf("first %s", log.errors[0])
	}
	if !strings.Contains(log.errors[1], "d-second") || !strings.Contains(log.errors[1], "v-second") {
		t.Fatalf("second %s", log.errors[1])
	}

	log.errors = nil
	one := &application.SnapshotOrderError{
		OrderID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", VariantID: "v", DesignID: "gone",
		Err: fmt.Errorf("metaldogs snapshot: design gone not found"),
	}
	logSnapshotFailure(job, one.OrderID, one)
	if len(log.errors) != 1 || !strings.Contains(log.errors[0], "gone") {
		t.Fatalf("build failure %+v", log.errors)
	}
}

type recordingJobs struct {
	ids      []string
	payloads []map[string]interface{}
}

func (r *recordingJobs) EnqueueManual(_ context.Context, name, jobID string, payload map[string]interface{}) error {
	if name != SnapshotOrderJobName {
		return fmt.Errorf("name %s", name)
	}
	r.ids = append(r.ids, jobID)
	r.payloads = append(r.payloads, payload)
	return nil
}

func (r *recordingJobs) QueueBound() bool         { return true }
func (r *recordingJobs) ReaderBound() bool        { return true }
func (r *recordingJobs) JobTypes(string) []string { return nil }
func (r *recordingJobs) CountsByType(context.Context, string) (map[ports.JobStatus]int, error) {
	return nil, nil
}
func (r *recordingJobs) Store() (ports.JobStore, bool) { return nil, false }
func (r *recordingJobs) DB() *sql.DB                   { return nil }

type sliceSyncLog struct {
	errors []string
}

func (l *sliceSyncLog) Info(string, map[string]interface{}) {}

func (l *sliceSyncLog) Error(msg string, _ error, fields map[string]interface{}) {
	l.errors = append(l.errors, fmt.Sprintf("%s %v", msg, fields))
}
