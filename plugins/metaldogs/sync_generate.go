package metaldogs

import (
	"context"
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
// Product claim fencing runs inside JobQueue.Requeue's transaction via OnRequeued
// (not a second post-commit InvalidateClaim).
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
	admin, ok := queue.(jobs.Admin)
	if !ok {
		return fmt.Errorf("metaldogs generate enqueue: job %s already exists but queue does not support Requeue (require queue.driver=postgres)", generationJobID)
	}
	_, err := admin.Requeue(ctx, generationJobID, domain.DefaultClaimLease)
	if err != nil {
		return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: %w", err)
	}
	// Claim fencing is owned by JobQueue.OnRequeued inside the re-pend transaction.
	return nil
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
