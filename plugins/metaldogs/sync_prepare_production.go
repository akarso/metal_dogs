package metaldogs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/observability"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/production"
)

const prepareProductionJobName = "prepare_production_artwork"

// prepareProductionMaxRetries lets a transient storage or database error retry.
// Platform Fail stays pending while attempts < max_retries. Permanent outcomes
// (pending_spec, validation) set max_retries = attempts so this delivery is the last.
// Reclaim of failed/cancelled still resets attempts and may raise max_retries;
// stale processing must leave attempts alone (zombie Fail fence).
const prepareProductionMaxRetries = 3

// prepareTerminalizeSQL ends retry for this processing row only.
const prepareTerminalizeSQL = `
		UPDATE jobs
		SET max_retries = attempts
		WHERE id = $1 AND status = 'processing' AND attempts >= 1 AND attempts < max_retries
	`

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

func registerPrepareProduction(reg *pluginsdk.SyncJobs, p *Plugin, log pluginsdk.Logger) error {
	return reg.RegisterManual(
		prepareProductionJobName,
		prepareProductionHandler(p, log),
		pluginsdk.MaxRetries(prepareProductionMaxRetries),
	)
}

func prepareProductionHandler(p *Plugin, log pluginsdk.Logger) extapi.SyncJobHandler {
	return func(ctx context.Context, job extapi.SyncJobContext) error {
		fields := observability.JobLogFields(job)
		versionID, _ := job.Payload["design_version_id"].(string)
		designID, _ := job.Payload["design_id"].(string)
		versionID = strings.TrimSpace(versionID)
		designID = strings.TrimSpace(designID)
		fields["design_version_id"] = versionID
		fields["design_id"] = designID
		var spec *production.Spec
		if p != nil {
			spec = p.productionSpec
		}
		if spec == nil {
			fields["pending_spec"] = true
			err := fmt.Errorf("metaldogs: prepare_production_artwork pending_spec")
			logPrepare(job, log, "metaldogs.prepare_production_artwork.pending_spec", err, fields)
			terminalizePrepareJob(ctx, job)
			if versionID == "" || designID == "" {
				return fmt.Errorf("metaldogs prepare: design_version_id and design_id required")
			}
			return err
		}
		if versionID == "" || designID == "" {
			return fmt.Errorf("metaldogs prepare: design_version_id and design_id required")
		}
		db := job.DB
		if db == nil && p != nil {
			bound, err := pluginDB(p)
			if err != nil {
				logPrepare(job, log, "metaldogs.prepare_production_artwork.retry", &application.PrepareRetryError{Op: "database"}, fields)
				return &application.PrepareRetryError{Op: "database"}
			}
			db = bound
		}
		if db == nil {
			logPrepare(job, log, "metaldogs.prepare_production_artwork.retry", &application.PrepareRetryError{Op: "database"}, fields)
			return &application.PrepareRetryError{Op: "database"}
		}
		copied := *spec
		var store ports.AssetStore
		var clock ports.Clock
		if p != nil {
			store = p.assets
			clock = p.snapshotClock
		}
		wrote, err := (&application.PrepareProduction{
			Versions: postgres.NewDesignVersionRepo(db),
			Assets:   postgres.NewAssetRepo(db),
			Designs:  postgres.NewDesignRepo(db),
			Store:    store,
			Spec:     &copied,
			Clock:    clock,
		}).Execute(ctx, versionID, designID)
		if err != nil {
			var permanent *application.PreparePermanentError
			if errors.As(err, &permanent) {
				fields["reason"] = permanent.Code
				logPrepare(job, log, "metaldogs.prepare_production_artwork.failed", permanent, fields)
				terminalizePrepareJob(ctx, job)
				return permanent
			}
			op := "database"
			var retry *application.PrepareRetryError
			if errors.As(err, &retry) && retry.Op != "" {
				op = retry.Op
			}
			fields["op"] = op
			wrapped := &application.PrepareRetryError{Op: op}
			logPrepare(job, log, "metaldogs.prepare_production_artwork.retry", wrapped, fields)
			return wrapped
		}
		if !wrote || p == nil {
			return nil
		}
		var ferr error
		if p.productionFill != nil {
			ferr = p.productionFill(ctx, versionID)
		} else {
			ferr = p.FillSnapshotProduction(ctx, versionID)
		}
		if ferr != nil {
			fields["op"] = "fill_snapshot"
			logPrepare(job, log, "metaldogs.prepare_production_artwork.fill_snapshot", ferr, fields)
		}
		return nil
	}
}

func logPrepare(job extapi.SyncJobContext, log pluginsdk.Logger, msg string, err error, fields map[string]interface{}) {
	switch {
	case job.Logger != nil:
		job.Logger.Error(msg, err, fields)
	case log != nil:
		log.Error(msg, err, fields)
	}
}

func terminalizePrepareJob(ctx context.Context, job extapi.SyncJobContext) {
	if job.DB == nil || strings.TrimSpace(job.JobID) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_, _ = job.DB.ExecContext(ctx, prepareTerminalizeSQL, job.JobID)
}

// enqueuePrepareProduction enqueues prepare_production_artwork with platform job id =
// design_version_id. On Conflict, never calls Admin.Retry (which would re-pend failed).
func enqueuePrepareProduction(jobs ports.JobAccess, ctx context.Context, designVersionID, designID string) error {
	if jobs == nil {
		return fmt.Errorf("metaldogs prepare enqueue: jobs access required")
	}
	designVersionID = strings.TrimSpace(designVersionID)
	designID = strings.TrimSpace(designID)
	if designVersionID == "" || designID == "" {
		return fmt.Errorf("metaldogs prepare enqueue: design_version_id and design_id required")
	}
	jobType, err := extapi.SyncJobType(PluginName, prepareProductionJobName)
	if err != nil {
		return fmt.Errorf("metaldogs prepare enqueue: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload := map[string]interface{}{
		"design_version_id": designVersionID,
		"design_id":         designID,
	}
	// Existing rows keep the prepare rules (failed/done stay, cancelled and stale
	// processing use the SQL reclaim). WithJobID would requeue every terminal row.
	if store, ok := jobs.Store(); ok {
		detail, err := store.Get(ctx, designVersionID)
		if err != nil {
			return fmt.Errorf("metaldogs prepare enqueue: get: %w", err)
		}
		if detail != nil {
			return resolvePrepareEnqueueConflict(jobs.DB(), store, ctx, designVersionID, jobType)
		}
	}
	if err := jobs.EnqueueManual(ctx, prepareProductionJobName, designVersionID, payload); err != nil {
		if pluginsdk.IsConflict(err) {
			store, ok := jobs.Store()
			if !ok {
				return fmt.Errorf("metaldogs prepare enqueue: job %s already exists but JobsPort is not bound (require CapJobs)", designVersionID)
			}
			return resolvePrepareEnqueueConflict(jobs.DB(), store, ctx, designVersionID, jobType)
		}
		return fmt.Errorf("metaldogs prepare enqueue: %w", err)
	}
	return nil
}

// resolvePrepareEnqueueConflict never uses Admin.Retry. Failed/done/pending are
// success. Young processing is left alone. Stale processing and cancelled are
// reclaimed with status-constrained UPDATEs (type-scoped).
func resolvePrepareEnqueueConflict(db *sql.DB, store ports.JobStore, ctx context.Context, designVersionID, jobType string) error {
	detail, err := store.Get(ctx, designVersionID)
	if err != nil {
		return fmt.Errorf("metaldogs prepare enqueue: get after conflict: %w", err)
	}
	if detail == nil {
		return fmt.Errorf("metaldogs prepare enqueue: job %s missing after conflict", designVersionID)
	}
	switch detail.Status {
	case ports.JobStatusDone, ports.JobStatusPending, ports.JobStatusFailed:
		return nil
	case ports.JobStatusCancelled:
		return reclaimCancelledPrepareJob(db, store, ctx, designVersionID, jobType)
	case ports.JobStatusProcessing:
		if !prepareJobStale(detail.UpdatedAt, domain.DefaultClaimLease) {
			return nil
		}
		return reclaimStaleProcessingPrepareJob(db, store, ctx, designVersionID, jobType)
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
func prepareReclaimRereadOK(reader jobLookup, ctx context.Context, designVersionID string) error {
	detail, err := reader.Get(ctx, designVersionID)
	if err != nil {
		return fmt.Errorf("metaldogs prepare enqueue: re-read after reclaim: %w", err)
	}
	if detail == nil {
		return fmt.Errorf("metaldogs prepare enqueue: job %s missing after reclaim", designVersionID)
	}
	switch detail.Status {
	case ports.JobStatusPending, ports.JobStatusProcessing, ports.JobStatusFailed, ports.JobStatusDone:
		return nil
	default:
		return fmt.Errorf("metaldogs prepare enqueue: reclaim unmatched, status=%s", detail.Status)
	}
}

// prepareStaleReclaimRereadOK is the zero-row path after a stale-processing UPDATE.
// Still-stale processing means the UPDATE did not match (type mismatch or app/DB
// clock skew) — error so approve returns 503 instead of a silent 200.
func prepareStaleReclaimRereadOK(reader jobLookup, ctx context.Context, designVersionID string) error {
	detail, err := reader.Get(ctx, designVersionID)
	if err != nil {
		return fmt.Errorf("metaldogs prepare enqueue: re-read after stale reclaim: %w", err)
	}
	if detail == nil {
		return fmt.Errorf("metaldogs prepare enqueue: job %s missing after stale reclaim", designVersionID)
	}
	switch detail.Status {
	case ports.JobStatusPending, ports.JobStatusFailed, ports.JobStatusDone:
		return nil
	case ports.JobStatusProcessing:
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

type jobLookup interface {
	Get(ctx context.Context, id string) (*ports.JobDetail, error)
}

// reclaimStaleProcessingPrepareJob re-pends only stale processing rows of this job type.
// Leaves attempts unchanged so a late Fail from the old delivery cannot match the next dequeue.
func reclaimStaleProcessingPrepareJob(db prepareJobExec, reader jobLookup, ctx context.Context, designVersionID, jobType string) error {
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
func reclaimCancelledPrepareJob(db prepareJobExec, reader jobLookup, ctx context.Context, designVersionID, jobType string) error {
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
