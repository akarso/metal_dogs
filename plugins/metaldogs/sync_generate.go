package metaldogs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/observability"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const generateJobName = "generate"

func registerGenerate(reg *pluginsdk.SyncJobs, worker *application.GenerateWorker, maxRetries int) error {
	if worker == nil {
		return fmt.Errorf("metaldogs generate: worker required")
	}
	opts := []pluginsdk.SyncJobOption{}
	if maxRetries > 0 {
		opts = append(opts, pluginsdk.MaxRetries(maxRetries))
	}
	return reg.RegisterManual(generateJobName, generateHandler(worker), opts...)
}

// enqueueGenerateIdempotent enqueues generate with generation_job_id as the job id.
// WithJobID owns the terminal-requeue race (one retry in the pin). A stale
// processing claim is requeued here. The product fence runs in Host.OnRequeued.
func enqueueGenerateIdempotent(jobs ports.JobAccess, ctx context.Context, generationJobID, designID string) error {
	if jobs == nil {
		return fmt.Errorf("metaldogs generate enqueue: app required")
	}
	generationJobID = strings.TrimSpace(generationJobID)
	designID = strings.TrimSpace(designID)
	if generationJobID == "" || designID == "" {
		return fmt.Errorf("metaldogs generate enqueue: generation_job_id and design_id required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload := map[string]interface{}{
		"generation_job_id": generationJobID,
		"design_id":         designID,
	}
	if err := jobs.EnqueueManual(ctx, generateJobName, generationJobID, payload); err != nil {
		if pluginsdk.IsConflict(err) {
			return fmt.Errorf("metaldogs generate enqueue: job %s already exists but queue does not support Requeue (require queue.driver=postgres)", generationJobID)
		}
		return fmt.Errorf("metaldogs generate enqueue: %w", err)
	}
	store, ok := jobs.Store()
	if !ok {
		return nil
	}
	// WithJobID leaves a processing row alone. A stale claim is requeued here.
	return requeueStaleGenerateClaim(store, ctx, generationJobID)
}

func requeueStaleGenerateClaim(store ports.JobStore, ctx context.Context, generationJobID string) error {
	detail, err := store.Get(ctx, generationJobID)
	if err != nil {
		return fmt.Errorf("metaldogs generate enqueue: stale claim: %w", err)
	}
	if detail == nil || detail.Status != ports.JobStatusProcessing {
		return nil
	}
	if !prepareJobStale(detail.UpdatedAt, domain.DefaultClaimLease) {
		return nil
	}
	err = store.Requeue(ctx, generationJobID, detail.Attempts, pluginsdk.WithRequeueMinAge(domain.DefaultClaimLease))
	if err == nil || errors.Is(err, pluginsdk.ErrRequeueTooFresh) {
		return nil
	}
	return fmt.Errorf("metaldogs generate enqueue: stale claim: %w", err)
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
