package metaldogs

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/observability"
)

const prepareProductionJobName = "prepare_production_artwork"

// prepareProductionMaxRetries: C6 stub always errors — MaxRetries=1 makes the first
// failure terminal (failed) without a 3-attempt backoff loop. G5 reclaim of
// failed/cancelled may set attempts=0 and raise max_retries together; stale
// processing must leave attempts alone (zombie Fail fence) and raise max_retries only.
const prepareProductionMaxRetries = 1

// Stale processing re-pends without touching attempts (matches platform Requeue).
const prepareStaleProcessingSQL = `
		UPDATE jobs
		SET status = 'pending', run_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND type = $2 AND status = 'processing'
		  AND updated_at < NOW() - make_interval(secs => $3)
	`

// Cancelled is not in flight — reset attempts so the next delivery starts clean.
const prepareCancelledSQL = `
		UPDATE jobs
		SET status = 'pending', attempts = 0, run_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND type = $2 AND status = 'cancelled'
	`

func registerPrepareProduction(app *plugin.App) error {
	sdk := pluginsdk.New(app, PluginName)
	return sdk.Integration(PluginName).RegisterManual(
		prepareProductionJobName,
		prepareProductionHandler(app),
		pluginsdk.MaxRetries(prepareProductionMaxRetries),
	)
}

func prepareProductionHandler(app *plugin.App) extapi.SyncJobHandler {
	return func(_ context.Context, job extapi.SyncJobContext) error {
		fields := observability.JobLogFields(job)
		fields["pending_spec"] = true
		versionID, _ := job.Payload["design_version_id"].(string)
		designID, _ := job.Payload["design_id"].(string)
		versionID = strings.TrimSpace(versionID)
		designID = strings.TrimSpace(designID)
		fields["design_version_id"] = versionID
		fields["design_id"] = designID
		err := fmt.Errorf("metaldogs: prepare_production_artwork pending_spec until G5/P0")
		switch {
		case job.Logger != nil:
			job.Logger.Error("metaldogs.prepare_production_artwork.pending_spec", err, fields)
		case app != nil && app.Logger != nil:
			app.Logger.Error("metaldogs.prepare_production_artwork.pending_spec", err, fields)
		}
		if versionID == "" || designID == "" {
			return fmt.Errorf("metaldogs prepare: design_version_id and design_id required")
		}
		return err
	}
}

// enqueuePrepareProduction enqueues prepare_production_artwork with platform job id =
// design_version_id. On Conflict, never calls Admin.Retry (which would re-pend failed).
func enqueuePrepareProduction(app *plugin.App, ctx context.Context, designVersionID, designID string) error {
	if app == nil {
		return fmt.Errorf("metaldogs prepare enqueue: app required")
	}
	designVersionID = strings.TrimSpace(designVersionID)
	designID = strings.TrimSpace(designID)
	if designVersionID == "" || designID == "" {
		return fmt.Errorf("metaldogs prepare enqueue: design_version_id and design_id required")
	}
	queue, ok := app.BoundQueue()
	if !ok || queue == nil {
		return fmt.Errorf("metaldogs prepare enqueue: %w", extapi.ErrSyncJobQueueUnbound)
	}
	jobType, err := extapi.SyncJobType(PluginName, prepareProductionJobName)
	if err != nil {
		return fmt.Errorf("metaldogs prepare enqueue: %w", err)
	}
	var reg plugin.SyncJobRegistration
	found := false
	for _, r := range app.SyncJobs() {
		if r.JobType == jobType {
			reg = r
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("metaldogs prepare enqueue: %w", extapi.ErrSyncJobNotFound)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload := plugin.InjectRequestID(ctx, map[string]interface{}{
		"design_version_id": designVersionID,
		"design_id":         designID,
	})
	job, err := plugin.NewSyncJobEnvelope(reg, extapi.SyncTriggerManual, payload)
	if err != nil {
		return fmt.Errorf("metaldogs prepare enqueue: envelope: %w", err)
	}
	job.ID = designVersionID
	if err := queue.Enqueue(ctx, job); err != nil {
		if apperror.Is(err, apperror.CodeConflict) {
			return resolvePrepareEnqueueConflict(app, queue, ctx, designVersionID, jobType)
		}
		return fmt.Errorf("metaldogs prepare enqueue: %w", err)
	}
	return nil
}

// resolvePrepareEnqueueConflict never uses Admin.Retry. Failed/done/pending are
// success. Young processing is left alone. Stale processing and cancelled are
// reclaimed with status-constrained UPDATEs (type-scoped).
func resolvePrepareEnqueueConflict(app *plugin.App, queue jobs.Queue, ctx context.Context, designVersionID, jobType string) error {
	reader, ok := queue.(jobs.Reader)
	if !ok {
		return fmt.Errorf("metaldogs prepare enqueue: job %s already exists but queue does not support Reader (require queue.driver=postgres)", designVersionID)
	}
	detail, err := reader.Get(ctx, designVersionID)
	if err != nil {
		return fmt.Errorf("metaldogs prepare enqueue: get after conflict: %w", err)
	}
	if detail == nil {
		return fmt.Errorf("metaldogs prepare enqueue: job %s missing after conflict", designVersionID)
	}
	switch detail.Status {
	case jobs.StatusDone, jobs.StatusPending, jobs.StatusFailed:
		return nil
	case jobs.StatusCancelled:
		return reclaimCancelledPrepareJob(prepareJobDB(app), reader, ctx, designVersionID, jobType)
	case jobs.StatusProcessing:
		if !prepareJobStale(detail.UpdatedAt, domain.DefaultClaimLease) {
			return nil
		}
		return reclaimStaleProcessingPrepareJob(prepareJobDB(app), reader, ctx, designVersionID, jobType)
	default:
		return fmt.Errorf("metaldogs prepare enqueue: unexpected status %s", detail.Status)
	}
}

func prepareJobStale(updatedAt time.Time, lease time.Duration) bool {
	if lease <= 0 {
		lease = domain.DefaultClaimLease
	}
	if updatedAt.IsZero() {
		return true
	}
	return time.Since(updatedAt) >= lease
}

// prepareReclaimRereadOK is success when the row is already in a stable or
// in-flight state after a status-constrained UPDATE matched zero rows.
func prepareReclaimRereadOK(reader jobs.Reader, ctx context.Context, designVersionID string) error {
	detail, err := reader.Get(ctx, designVersionID)
	if err != nil {
		return fmt.Errorf("metaldogs prepare enqueue: re-read after reclaim: %w", err)
	}
	if detail == nil {
		return fmt.Errorf("metaldogs prepare enqueue: job %s missing after reclaim", designVersionID)
	}
	switch detail.Status {
	case jobs.StatusPending, jobs.StatusProcessing, jobs.StatusFailed, jobs.StatusDone:
		return nil
	default:
		return fmt.Errorf("metaldogs prepare enqueue: reclaim unmatched, status=%s", detail.Status)
	}
}

// prepareStaleReclaimRereadOK is the zero-row path after a stale-processing UPDATE.
// Still-stale processing means the UPDATE did not match (type mismatch or app/DB
// clock skew) — error so approve returns 503 instead of a silent 200.
func prepareStaleReclaimRereadOK(reader jobs.Reader, ctx context.Context, designVersionID string) error {
	detail, err := reader.Get(ctx, designVersionID)
	if err != nil {
		return fmt.Errorf("metaldogs prepare enqueue: re-read after stale reclaim: %w", err)
	}
	if detail == nil {
		return fmt.Errorf("metaldogs prepare enqueue: job %s missing after stale reclaim", designVersionID)
	}
	switch detail.Status {
	case jobs.StatusPending, jobs.StatusFailed, jobs.StatusDone:
		return nil
	case jobs.StatusProcessing:
		if prepareJobStale(detail.UpdatedAt, domain.DefaultClaimLease) {
			return fmt.Errorf("metaldogs prepare enqueue: still stale processing after reclaim (type mismatch or clock skew)")
		}
		return nil
	default:
		return fmt.Errorf("metaldogs prepare enqueue: stale reclaim unmatched, status=%s", detail.Status)
	}
}

// prepareJobExec is *sql.DB (or a test double) for reclaim UPDATEs.
type prepareJobExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// reclaimStaleProcessingPrepareJob re-pends only stale processing rows of this job type.
// Leaves attempts unchanged so a late Fail from the old delivery cannot match the next dequeue.
func reclaimStaleProcessingPrepareJob(db prepareJobExec, reader jobs.Reader, ctx context.Context, designVersionID, jobType string) error {
	if db == nil {
		return fmt.Errorf("metaldogs prepare enqueue: stale processing reclaim requires Bootstrap.DB")
	}
	secs := int64(domain.DefaultClaimLease.Seconds())
	if secs < 1 {
		secs = 1
	}
	res, err := db.ExecContext(ctx, prepareStaleProcessingSQL, designVersionID, jobType, secs)
	if err != nil {
		return fmt.Errorf("metaldogs prepare enqueue: reclaim stale processing: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	return prepareStaleReclaimRereadOK(reader, ctx, designVersionID)
}

// reclaimCancelledPrepareJob sets cancelled → pending with attempts=0 for this job type only.
func reclaimCancelledPrepareJob(db prepareJobExec, reader jobs.Reader, ctx context.Context, designVersionID, jobType string) error {
	if db == nil {
		return fmt.Errorf("metaldogs prepare enqueue: cancelled reclaim requires Bootstrap.DB")
	}
	res, err := db.ExecContext(ctx, prepareCancelledSQL, designVersionID, jobType)
	if err != nil {
		return fmt.Errorf("metaldogs prepare enqueue: reclaim cancelled: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	return prepareReclaimRereadOK(reader, ctx, designVersionID)
}

func prepareJobDB(app *plugin.App) *sql.DB {
	if app == nil || app.Bootstrap == nil {
		return nil
	}
	return app.Bootstrap.DB
}
