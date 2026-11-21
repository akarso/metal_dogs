package ports

import (
	"context"
	"database/sql"

	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Job status values owned by the plugin. They are the pluginsdk/job-queue statuses.
const (
	JobStatusPending    = pluginsdk.JobStatusPending
	JobStatusProcessing = pluginsdk.JobStatusProcessing
	JobStatusDone       = pluginsdk.JobStatusDone
	JobStatusFailed     = pluginsdk.JobStatusFailed
	JobStatusReclaiming = pluginsdk.JobStatusReclaiming
	JobStatusCancelled  = pluginsdk.JobStatusCancelled
)

// JobDetail is one queue row (Get).
type JobDetail = pluginsdk.JobDetail

// JobStatus is a queue lifecycle status.
type JobStatus = pluginsdk.JobStatus

// RequeueOption configures JobStore.Requeue.
type RequeueOption = pluginsdk.RequeueOption

// JobStore is the read/requeue surface of a bound Postgres queue.
type JobStore interface {
	Get(ctx context.Context, id string) (*JobDetail, error)
	Requeue(ctx context.Context, id string, attempt int, opts ...RequeueOption) error
}

// JobHealth is the customizer health probe (bound queue, reader, per-type depths).
type JobHealth interface {
	QueueBound() bool
	ReaderBound() bool
	JobTypes(pluginSlug string) []string
	CountsByType(ctx context.Context, jobType string) (map[JobStatus]int, error)
}

// JobAccess is enqueue, conflict lookup, and health. PlatformJobs adapts pluginsdk.Host.
type JobAccess interface {
	JobHealth
	EnqueueManual(ctx context.Context, name, jobID string, payload map[string]interface{}) error
	Store() (JobStore, bool)
	DB() *sql.DB
}
