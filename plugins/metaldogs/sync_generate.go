package metaldogs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/observability"
)

const generateJobName = "generate"

func registerGenerate(app *plugin.App, worker *application.GenerateWorker, maxRetries int) error {
	if worker == nil {
		return fmt.Errorf("metaldogs generate: worker required")
	}
	sdk := pluginsdk.New(app, PluginName)
	opts := []pluginsdk.SyncJobOption{}
	if maxRetries > 0 {
		opts = append(opts, pluginsdk.MaxRetries(maxRetries))
	}
	return sdk.Integration(PluginName).RegisterManual(generateJobName, generateHandler(worker), opts...)
}

// enqueueGenerateIdempotent enqueues integration.sync.metaldogs.generate using the
// product generation_job_id as the platform job primary key. On Conflict it loads
// the existing row (Postgres Reader/Admin) and re-pends failed/done/cancelled or
// processing older than the stage lease; pending / young processing are left alone.
// Product claim fencing runs in Host.OnRequeued (pluginsdk.RequeueTx, same
// transaction as the re-pend). Stale processing uses WithRequeueMinAge.
// Failed, done, and cancelled use WithRequeueTerminal. Pending and a young
// processing claim are left alone.
func enqueueGenerateIdempotent(app *plugin.App, ctx context.Context, generationJobID, designID string) error {
	if app == nil {
		return fmt.Errorf("metaldogs generate enqueue: app required")
	}
	generationJobID = strings.TrimSpace(generationJobID)
	designID = strings.TrimSpace(designID)
	if generationJobID == "" || designID == "" {
		return fmt.Errorf("metaldogs generate enqueue: generation_job_id and design_id required")
	}
	queue, ok := app.BoundQueue()
	if !ok || queue == nil {
		return fmt.Errorf("metaldogs generate enqueue: %w", extapi.ErrSyncJobQueueUnbound)
	}
	jobType, err := extapi.SyncJobType(PluginName, generateJobName)
	if err != nil {
		return fmt.Errorf("metaldogs generate enqueue: %w", err)
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
		return fmt.Errorf("metaldogs generate enqueue: %w", extapi.ErrSyncJobNotFound)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload := plugin.InjectRequestID(ctx, map[string]interface{}{
		"generation_job_id": generationJobID,
		"design_id":         designID,
	})
	job, err := plugin.NewSyncJobEnvelope(reg, extapi.SyncTriggerManual, payload)
	if err != nil {
		return fmt.Errorf("metaldogs generate enqueue: envelope: %w", err)
	}
	job.ID = generationJobID
	if err := queue.Enqueue(ctx, job); err != nil {
		if apperror.Is(err, apperror.CodeConflict) {
			return resolveGenerateEnqueueConflict(queue, ctx, generationJobID)
		}
		return fmt.Errorf("metaldogs generate enqueue: %w", err)
	}
	return nil
}

func resolveGenerateEnqueueConflict(queue jobs.Queue, ctx context.Context, generationJobID string) error {
	reader, ok := queue.(jobs.Reader)
	if !ok {
		return fmt.Errorf("metaldogs generate enqueue: job %s already exists but queue does not support Requeue (require queue.driver=postgres)", generationJobID)
	}
	detail, err := reader.Get(ctx, generationJobID)
	if err != nil {
		return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: %w", err)
	}
	if detail == nil {
		return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: job %s missing", generationJobID)
	}
	err = queue.Requeue(ctx, generationJobID, detail.Attempts, jobs.WithRequeueMinAge(domain.DefaultClaimLease))
	if err == nil || errors.Is(err, jobs.ErrRequeueTooFresh) {
		return nil
	}
	if errors.Is(err, jobs.ErrRequeueNotProcessing) {
		termErr := queue.Requeue(ctx, generationJobID, detail.Attempts, jobs.WithRequeueTerminal())
		if termErr == nil {
			return nil
		}
		if errors.Is(termErr, jobs.ErrRequeueNotTerminal) {
			return leaveNonTerminalGenerateJob(reader, ctx, generationJobID)
		}
		return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: %w", termErr)
	}
	return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: %w", err)
}

// leaveNonTerminalGenerateJob accepts pending and processing. Any other
// non-terminal status (for example reclaiming) is still an error.
func leaveNonTerminalGenerateJob(reader jobs.Reader, ctx context.Context, id string) error {
	again, err := reader.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: %w", err)
	}
	if again == nil {
		return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: job %s missing", id)
	}
	switch again.Status {
	case jobs.StatusPending, jobs.StatusProcessing:
		return nil
	default:
		return fmt.Errorf("metaldogs generate enqueue: requeue terminal job %s is %s", id, again.Status)
	}
}

func generateHandler(worker *application.GenerateWorker) extapi.SyncJobHandler {
	return func(ctx context.Context, job extapi.SyncJobContext) error {
		fields := observability.JobLogFields(job)
		jobID, _ := job.Payload["generation_job_id"].(string)
		jobID = strings.TrimSpace(jobID)
		designID, _ := job.Payload["design_id"].(string)
		designID = strings.TrimSpace(designID)
		if jobID == "" {
			if job.Logger != nil {
				job.Logger.Error("metaldogs.generate.missing_job_id", fmt.Errorf("generation_job_id required"), fields)
			}
			return nil
		}
		startFields := map[string]interface{}{
			"generation_job_id": jobID,
			"design_id":         designID,
			"attempt":           job.Attempts,
		}
		for k, v := range fields {
			startFields[k] = v
		}
		if job.Logger != nil {
			job.Logger.Info("metaldogs.generate.start", startFields)
		}
		start := time.Now()
		err := worker.Process(ctx, jobID, designID, job.Attempts)
		durationMS := time.Since(start).Milliseconds()
		doneFields := map[string]interface{}{
			"generation_job_id": jobID,
			"design_id":         designID,
			"attempt":           job.Attempts,
			"duration_ms":       durationMS,
		}
		for k, v := range fields {
			doneFields[k] = v
		}
		if err != nil {
			if job.Logger != nil {
				job.Logger.Error("metaldogs.generate.failed", err, doneFields)
			}
			return err
		}
		if job.Logger != nil {
			job.Logger.Info("metaldogs.generate.done", doneFields)
		}
		return nil
	}
}
