package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"github.com/akarso/shopanda/plugins/metaldogs/generation/validate"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// ArtworkRetention is how long generated artwork blobs are kept (aligned with design retention).
const ArtworkRetention = domain.DesignRetentionUnordered

// ErrTransientGeneration signals the Shopanda queue should retry (Fail path).
var ErrTransientGeneration = errors.New("transient generation failure")

const generateStageTimeout = 2 * time.Minute

// GenerateWorker runs integration.sync.metaldogs.generate stages.
type GenerateWorker struct {
	Designs   ports.DesignRepository
	Jobs      ports.GenerationJobRepository
	Assets    ports.AssetRepository
	Templates ports.TemplateStore
	Store     ports.AssetStore
	Provider  generation.ImageProvider
	IDs       ports.IDGenerator
	Clock     ports.Clock
	// MaxQueueAttempts bounds platform retries (dequeue Attempts vs MaxRetries).
	MaxQueueAttempts int
}

// Process runs one sync-job delivery for generation_job_id.
// designIDHint comes from the platform payload and is used when the job row cannot be loaded
// on the last attempt (so the design can still be freed).
func (w *GenerateWorker) Process(ctx context.Context, generationJobID, designIDHint string, queueAttempts int) error {
	if w == nil || w.Designs == nil || w.Jobs == nil || w.Assets == nil || w.Templates == nil ||
		w.Store == nil || w.Provider == nil || w.IDs == nil || w.Clock == nil {
		return fmt.Errorf("generate worker: deps missing")
	}
	generationJobID = strings.TrimSpace(generationJobID)
	designIDHint = strings.TrimSpace(designIDHint)
	if generationJobID == "" {
		return fmt.Errorf("generate worker: generation_job_id required")
	}

	maxAttempts := w.MaxQueueAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	job, ok, err := w.Jobs.GetByID(ctx, generationJobID)
	if err != nil {
		return w.maybeLastAttemptLoadJob(ctx, generationJobID, designIDHint, queueAttempts, maxAttempts, err)
	}
	if !ok {
		return nil
	}
	if job.IsTerminal() {
		return nil
	}
	if job.ArtworkAssetID != "" {
		return w.finishAlreadyStored(ctx, job)
	}

	claimed, err := w.Jobs.ClaimForWork(ctx, job.ID, generateStageTimeout)
	if err != nil {
		if errors.Is(err, ports.ErrGenerationJobStale) {
			// Peer holds the claim — do not Complete/Fail this platform delivery.
			return jobs.ErrSkipOutcome
		}
		return w.maybeLastAttempt(ctx, job, queueAttempts, maxAttempts, domain.TerminalStorageError, "claim_running", err)
	}
	job = claimed
	now := w.Clock.Now().UTC()
	if job.StartedAt != nil {
		now = job.StartedAt.UTC()
	}

	stageCtx, cancel := context.WithTimeout(ctx, generateStageTimeout)
	defer cancel()

	design, ok, err := w.Designs.GetByID(stageCtx, job.DesignID)
	if err != nil {
		return w.maybeLastAttempt(ctx, job, queueAttempts, maxAttempts, domain.TerminalStorageError, "load_design", err)
	}
	if !ok {
		return w.failPermanent(ctx, job, domain.TerminalCancelled, domain.TerminalCancelled)
	}
	if design.Status != domain.DesignGenerating {
		return w.failPermanent(ctx, job, domain.TerminalCancelled, domain.TerminalCancelled)
	}

	tpl, ok, err := w.Templates.GetByID(stageCtx, design.TemplateID)
	if err != nil {
		return w.maybeLastAttempt(ctx, job, queueAttempts, maxAttempts, domain.TerminalStorageError, "load_template", err)
	}
	if !ok || tpl.Version != design.TemplateVersion {
		return w.failPermanent(ctx, job, domain.TerminalProviderRejectedInput, "template_missing")
	}

	source, ok, err := w.Assets.GetByID(stageCtx, design.SourceAssetID)
	if err != nil {
		return w.maybeLastAttempt(ctx, job, queueAttempts, maxAttempts, domain.TerminalStorageError, "load_source", err)
	}
	if !ok || source.StorageKey == "" {
		return w.failPermanent(ctx, job, domain.TerminalProviderRejectedInput, "source_missing")
	}
	photoBytes, err := w.Store.Read(stageCtx, source.StorageKey)
	if err != nil {
		return w.transientOrFail(ctx, job, domain.TerminalStorageError, "storage_read", err, queueAttempts, maxAttempts)
	}

	req, err := buildGenerationRequest(design, tpl, photoBytes, w.Provider.Name())
	if err != nil {
		return w.failPermanent(ctx, job, domain.TerminalProviderRejectedInput, "prepare_failed")
	}

	if err := w.ensureClaimHeld(ctx, stageCtx, job, queueAttempts, maxAttempts); err != nil {
		return err
	}
	result, err := w.Provider.Generate(stageCtx, req)
	if err != nil {
		return w.mapProviderError(ctx, job, err, queueAttempts, maxAttempts)
	}
	if err := w.ensureClaimHeld(ctx, stageCtx, job, queueAttempts, maxAttempts); err != nil {
		return err
	}

	paramsJSON, err := json.Marshal(req.Parameters)
	if err != nil {
		paramsJSON = []byte("{}")
	}
	job.Model = result.Model
	job.PromptVersion = req.PromptVersion
	job.ParametersJSON = paramsJSON

	// Validate in-memory before any durable stage that forbids another provider call.
	vr := validate.Validate(result.ImagePNG, tpl.OutputConfig)
	vrJSON, err := json.Marshal(vr)
	if err != nil {
		vrJSON = []byte(`{"status":"fail"}`)
	}
	if vr.Failed() {
		job.ValidationJSON = vrJSON
		return w.failPermanent(ctx, job, domain.TerminalValidationFailed, validatePrimaryCode(vr))
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(result.ImagePNG))
	if err != nil {
		return w.failPermanent(ctx, job, domain.TerminalValidationFailed, validate.CodeDecodeFailed)
	}

	// Persist PNG before committing validating/succeeded so a crash can re-call provider
	// (no artwork_asset_id yet) without orphaning a "validating" row that cannot MarkValidating.
	assetID := w.IDs.New()
	key, err := ports.ObjectKey(domain.AssetTypeArtwork, assetID, "png", now)
	if err != nil {
		return w.failPermanent(ctx, job, domain.TerminalStorageError, "object_key")
	}
	sum := sha256.Sum256(result.ImagePNG)
	cost := float64(result.CostMinorUnits) / 100.0
	artwork := domain.Asset{
		ID:          assetID,
		Type:        domain.AssetTypeArtwork,
		StorageKey:  key,
		MimeType:    "image/png",
		SizeBytes:   int64(len(result.ImagePNG)),
		Width:       cfg.Width,
		Height:      cfg.Height,
		Checksum:    ChecksumSHA256Prefix + hex.EncodeToString(sum[:]),
		CreatedAt:   now,
		DeleteAfter: design.ExpiresAt,
		OwnerRef:    design.ID,
	}
	if artwork.DeleteAfter.IsZero() || artwork.DeleteAfter.Before(now) {
		artwork.DeleteAfter = now.Add(ArtworkRetention)
	}
	if err := w.ensureClaimHeld(ctx, stageCtx, job, queueAttempts, maxAttempts); err != nil {
		return err
	}
	if err := w.Store.Put(stageCtx, key, bytes.NewReader(result.ImagePNG), "image/png"); err != nil {
		return w.transientOrFail(ctx, job, domain.TerminalStorageError, "storage_put", err, queueAttempts, maxAttempts)
	}

	latencyMS := int(result.Latency.Milliseconds())
	if err := job.Succeed(assetID, vrJSON, latencyMS, &cost, "USD", job.Model, job.PromptVersion, job.ParametersJSON, now); err != nil {
		_ = w.Store.Delete(stageCtx, key)
		return w.forceFailPermanent(ctx, job, domain.TerminalCancelled, "invalid_transition")
	}
	if err := design.MarkPreviewReady(now); err != nil {
		_ = w.Store.Delete(stageCtx, key)
		return w.forceFailPermanent(ctx, job, domain.TerminalCancelled, "design_transition")
	}
	if err := w.ensureClaimHeld(ctx, stageCtx, job, queueAttempts, maxAttempts); err != nil {
		_ = w.Store.Delete(stageCtx, key)
		return err
	}
	if err := w.Jobs.CompleteSuccess(ctx, job, artwork, design); err != nil {
		_ = w.Store.Delete(stageCtx, key)
		if errors.Is(err, ports.ErrGenerationJobStale) {
			return jobs.ErrSkipOutcome
		}
		if errors.Is(err, ports.ErrDesignConflict) {
			return w.forceFailPermanent(ctx, job, domain.TerminalCancelled, "design_conflict")
		}
		return w.maybeLastAttempt(ctx, job, queueAttempts, maxAttempts, domain.TerminalStorageError, "complete_success", err)
	}
	return nil
}

// ensureClaimHeld re-reads the product job before paid work or durable writes.
// Lost/inactive claim_token → ErrSkipOutcome (even if stageCtx is already done).
// Still holding the claim with a canceled stage or a failed re-read →
// transientOrFail/failPermanent so the product row leaves running before the
// platform Fail, and the next delivery can ClaimForWork without waiting out the lease.
func (w *GenerateWorker) ensureClaimHeld(parentCtx, stageCtx context.Context, job domain.GenerationJob, queueAttempts, maxAttempts int) error {
	readCtx := parentCtx
	if readCtx.Err() != nil {
		readCtx = context.WithoutCancel(parentCtx)
	}
	cur, ok, err := w.Jobs.GetByID(readCtx, job.ID)
	if err != nil {
		return w.transientOrFail(parentCtx, job, domain.TerminalStorageError, "claim_check", err, queueAttempts, maxAttempts)
	}
	if !ok || !cur.IsActive() || cur.ClaimToken != job.ClaimToken {
		return jobs.ErrSkipOutcome
	}
	if err := stageCtx.Err(); err != nil {
		terminal := domain.TerminalCancelled
		code := "stage_canceled"
		if errors.Is(err, context.DeadlineExceeded) {
			terminal = domain.TerminalProviderTimeout
			code = "stage_timeout"
		}
		return w.transientOrFail(parentCtx, job, terminal, code, err, queueAttempts, maxAttempts)
	}
	return nil
}

func (w *GenerateWorker) finishAlreadyStored(ctx context.Context, job domain.GenerationJob) error {
	if job.Status == domain.JobSucceeded {
		return nil
	}
	now := w.Clock.Now().UTC()
	design, ok, err := w.Designs.GetByID(ctx, job.DesignID)
	if err != nil {
		return err
	}
	latency := 0
	if job.LatencyMS != nil {
		latency = *job.LatencyMS
	}
	if err := job.Succeed(job.ArtworkAssetID, job.ValidationJSON, latency, job.Cost, job.CostCurrency, job.Model, job.PromptVersion, job.ParametersJSON, now); err != nil {
		job.Status = domain.JobSucceeded
		job.FinishedAt = &now
	}
	if !ok {
		err := w.Jobs.FinalizeSuccess(ctx, job, domain.Design{ID: job.DesignID, Status: domain.DesignPreviewReady, UpdatedAt: now})
		if errors.Is(err, ports.ErrGenerationJobStale) {
			return jobs.ErrSkipOutcome
		}
		return err
	}
	if design.Status == domain.DesignGenerating {
		_ = design.MarkPreviewReady(now)
	}
	err = w.Jobs.FinalizeSuccess(ctx, job, design)
	if errors.Is(err, ports.ErrGenerationJobStale) {
		return jobs.ErrSkipOutcome
	}
	return err
}

func (w *GenerateWorker) mapProviderError(ctx context.Context, job domain.GenerationJob, err error, queueAttempts, maxAttempts int) error {
	class := generation.ClassifyError(err)
	switch class {
	case generation.ErrorTransient, generation.ErrorRateLimited, generation.ErrorCanceled:
		reason := domain.TerminalProviderError
		code := string(class)
		if class == generation.ErrorRateLimited {
			reason = domain.TerminalProviderRateLimited
		}
		if errors.Is(err, context.DeadlineExceeded) {
			reason = domain.TerminalProviderTimeout
			code = "timeout"
		}
		return w.transientOrFail(ctx, job, reason, code, err, queueAttempts, maxAttempts)
	case generation.ErrorRejectedInput:
		return w.failPermanent(ctx, job, domain.TerminalProviderRejectedInput, string(class))
	default:
		return w.failPermanent(ctx, job, domain.TerminalProviderError, string(class))
	}
}

func (w *GenerateWorker) transientOrFail(ctx context.Context, job domain.GenerationJob, terminal, code string, cause error, queueAttempts, maxAttempts int) error {
	if queueAttempts >= maxAttempts {
		if terminal == "" {
			terminal = domain.TerminalAttemptsExhausted
		}
		return w.failPermanent(ctx, job, terminal, code)
	}
	expected := job.Status
	if err := job.RequeueForRetry(); err != nil {
		return w.failPermanent(ctx, job, terminal, code)
	}
	if err := w.Jobs.Update(ctx, job, expected); err != nil {
		if errors.Is(err, ports.ErrGenerationJobStale) {
			return jobs.ErrSkipOutcome
		}
		return fmt.Errorf("%w: requeue: %v (cause: %v)", ErrTransientGeneration, err, cause)
	}
	return fmt.Errorf("%w: %s: %v", ErrTransientGeneration, code, cause)
}

func (w *GenerateWorker) maybeLastAttemptLoadJob(ctx context.Context, generationJobID, designIDHint string, queueAttempts, maxAttempts int, cause error) error {
	if queueAttempts < maxAttempts {
		return fmt.Errorf("%w: load_job: %v", ErrTransientGeneration, cause)
	}
	// Last attempt: re-read. Never FailActive without a claim_token, and never
	// ack the platform job if the follow-up read still fails.
	job, ok, err := w.Jobs.GetByID(ctx, generationJobID)
	if err != nil {
		return fmt.Errorf("generate worker: load job on last attempt: %w", err)
	}
	if !ok {
		return nil
	}
	if job.ArtworkAssetID != "" {
		return w.finishAlreadyStored(ctx, job)
	}
	if designIDHint != "" && job.DesignID == "" {
		job.DesignID = designIDHint
	}
	return w.failPermanent(ctx, job, domain.TerminalStorageError, "load_job")
}

func (w *GenerateWorker) maybeLastAttempt(ctx context.Context, job domain.GenerationJob, queueAttempts, maxAttempts int, terminal, code string, cause error) error {
	if queueAttempts >= maxAttempts && job.ID != "" {
		if err := w.failPermanent(ctx, job, terminal, code); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("%w: %s: %v", ErrTransientGeneration, code, cause)
}

func (w *GenerateWorker) failPermanent(ctx context.Context, job domain.GenerationJob, terminal, code string) error {
	now := w.Clock.Now().UTC()
	expected := job.Status
	if err := job.Fail(terminal, code, now); err != nil {
		return w.forceFailPermanent(ctx, job, terminal, code)
	}
	design, ok, err := w.Designs.GetByID(ctx, job.DesignID)
	if err != nil {
		// Job row already carries design_id — terminalize without the loaded aggregate.
		if cerr := w.Jobs.CompleteFailure(ctx, job, domain.Design{
			ID: job.DesignID, Status: domain.DesignGenerationFailed, UpdatedAt: now,
		}); cerr != nil {
			if errors.Is(cerr, ports.ErrGenerationJobStale) {
				return jobs.ErrSkipOutcome
			}
			return fmt.Errorf("generate worker: complete failure after design load error: %v (design: %w)", cerr, err)
		}
		return nil
	}
	if ok && design.Status == domain.DesignGenerating {
		_ = design.MarkGenerationFailed(now)
		if err := w.Jobs.CompleteFailure(ctx, job, design); err != nil {
			if errors.Is(err, ports.ErrGenerationJobStale) {
				return jobs.ErrSkipOutcome
			}
			return fmt.Errorf("generate worker: complete failure: %w", err)
		}
		return nil
	}
	if err := w.Jobs.Update(ctx, job, expected); err != nil {
		if errors.Is(err, ports.ErrGenerationJobStale) {
			return jobs.ErrSkipOutcome
		}
		return err
	}
	return nil
}

func (w *GenerateWorker) forceFailPermanent(ctx context.Context, job domain.GenerationJob, terminal, code string) error {
	now := w.Clock.Now().UTC()
	job.ForceFail(terminal, code, now)
	if job.DesignID == "" {
		return nil
	}
	design, ok, err := w.Designs.GetByID(ctx, job.DesignID)
	if err != nil || !ok {
		err = w.Jobs.CompleteFailure(ctx, job, domain.Design{
			ID: job.DesignID, Status: domain.DesignGenerationFailed, UpdatedAt: now,
		})
		if errors.Is(err, ports.ErrGenerationJobStale) {
			return jobs.ErrSkipOutcome
		}
		return err
	}
	if design.Status == domain.DesignGenerating {
		_ = design.MarkGenerationFailed(now)
	}
	err = w.Jobs.CompleteFailure(ctx, job, design)
	if errors.Is(err, ports.ErrGenerationJobStale) {
		return jobs.ErrSkipOutcome
	}
	return err
}

func buildGenerationRequest(d domain.Design, tpl domain.Template, photo []byte, providerName string) (generation.GenerationRequest, error) {
	dogName := strings.TrimSpace(d.Texts["dog_name"])
	params := generation.Parameters{
		Width:  tpl.ModelParameters.Width,
		Height: tpl.ModelParameters.Height,
		Seed:   tpl.ModelParameters.Seed,
	}
	if params.Width <= 0 {
		params.Width = tpl.OutputConfig.CanvasWidth
	}
	if params.Height <= 0 {
		params.Height = tpl.OutputConfig.CanvasHeight
	}
	params.Normalize()
	if err := params.Validate(); err != nil {
		return generation.GenerationRequest{}, err
	}
	prompt := strings.TrimSpace(tpl.PromptInstructions.Text)
	if prompt == "" {
		return generation.GenerationRequest{}, fmt.Errorf("empty prompt")
	}
	model := strings.TrimSpace(tpl.ModelParameters.Model)
	return generation.GenerationRequest{
		PhotoBytes:    photo,
		Prompt:        prompt,
		PromptVersion: tpl.PromptInstructions.PromptVersion,
		DogName:       dogName,
		Provider:      providerName,
		Model:         model,
		Parameters:    params,
	}, nil
}

func validatePrimaryCode(vr validate.Result) string {
	if len(vr.Codes) > 0 {
		return vr.Codes[0]
	}
	return domain.TerminalValidationFailed
}
