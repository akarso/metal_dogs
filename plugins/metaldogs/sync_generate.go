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
// WithJobID resolves one conflict. A raced Requeue (wrong attempt, or no longer
// terminal) is recovered from a fresh Get. A stale processing claim is requeued
// after success. The product fence runs in Host.OnRequeued.
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
		if errors.Is(err, pluginsdk.ErrRequeueWrongAttempt) || errors.Is(err, pluginsdk.ErrRequeueNotTerminal) {
			store, ok := jobs.Store()
			if !ok {
				return fmt.Errorf("metaldogs generate enqueue: %w", err)
			}
			return recoverGenerateEnqueueRace(store, ctx, generationJobID, err)
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

// recoverGenerateEnqueueRace handles a WithJobID terminal Requeue that lost a race.
// Wrong attempt retries once from a fresh Get. Not terminal accepts pending and processing.
func recoverGenerateEnqueueRace(store ports.JobStore, ctx context.Context, generationJobID string, enqueueErr error) error {
	if errors.Is(enqueueErr, pluginsdk.ErrRequeueNotTerminal) && !errors.Is(enqueueErr, pluginsdk.ErrRequeueWrongAttempt) {
		return leaveNonTerminalGenerateJob(store, ctx, generationJobID)
	}
	detail, err := store.Get(ctx, generationJobID)
	if err != nil {
		return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: %w", err)
	}
	if detail == nil {
		return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: job %s missing", generationJobID)
	}
	return requeueTerminalGenerateJob(store, ctx, generationJobID, detail.Attempts)
}

// A wrong attempt is a raced completion: reload once and retry with the live count.
func requeueTerminalGenerateJob(store ports.JobStore, ctx context.Context, id string, attempt int) error {
	err := requeueTerminalGenerateOnce(store, ctx, id, attempt)
	if err == nil || !errors.Is(err, pluginsdk.ErrRequeueWrongAttempt) {
		return err
	}
	again, getErr := store.Get(ctx, id)
	if getErr != nil {
		return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: %w", getErr)
	}
	if again == nil {
		return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: job %s missing", id)
	}
	if again.Attempts == attempt {
		return err
	}
	return requeueTerminalGenerateOnce(store, ctx, id, again.Attempts)
}

func requeueTerminalGenerateOnce(store ports.JobStore, ctx context.Context, id string, attempt int) error {
	err := store.Requeue(ctx, id, attempt, pluginsdk.WithRequeueTerminal())
	if err == nil {
		return nil
	}
	if errors.Is(err, pluginsdk.ErrRequeueNotTerminal) {
		return leaveNonTerminalGenerateJob(store, ctx, id)
	}
	return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: %w", err)
}

// leaveNonTerminalGenerateJob accepts pending and processing. Any other
// non-terminal status (for example reclaiming) is still an error.
func leaveNonTerminalGenerateJob(store ports.JobStore, ctx context.Context, id string) error {
	again, err := store.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: %w", err)
	}
	if again == nil {
		return fmt.Errorf("metaldogs generate enqueue: requeue after conflict: job %s missing", id)
	}
	switch again.Status {
	case ports.JobStatusPending, ports.JobStatusProcessing:
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
