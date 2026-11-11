package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// Generate error codes (HTTP-mapped).
const (
	GenerateErrUnavailable       = "unavailable"
	GenerateErrNotFound          = "not_found"
	GenerateErrBadRequest        = "bad_request"
	GenerateErrInvalidTransition = "invalid_transition"
	GenerateErrConflict          = "conflict"
	GenerateErrLimitReached      = "limit_reached"
	GenerateErrCooldown          = "cooldown"
	GenerateErrUploadUnavailable = "upload_unavailable"
)

// GenerateError is a classified generate failure for HTTP mapping.
type GenerateError struct {
	Code       string
	Message    string
	JobID      string // set on conflict when an active/prior job exists
	DesignID   string
	RetryAfter int // seconds; set for cooldown / limit hints
}

func (e *GenerateError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

// JobView is the customer-facing generation job projection.
type JobView struct {
	ID             string
	DesignID       string
	Status         string
	Attempt        int
	ErrorCode      string
	TerminalReason string
	RetryAfter     int
}

// GenerateService coordinates POST generate + GET job polling.
type GenerateService struct {
	Designs     ports.DesignRepository
	Jobs        ports.GenerationJobRepository
	Assets      ports.AssetRepository
	IDs         ports.IDGenerator
	Clock       ports.Clock
	Enqueue     ports.GenerationEnqueue
	Provider    string
	MaxAttempts int
	// Cooldown is the minimum gap between distinct generation attempts (C7). 0 → default.
	Cooldown time.Duration
}

func (s *GenerateService) depsReady() bool {
	return s != nil && s.Designs != nil && s.Jobs != nil && s.Assets != nil &&
		s.IDs != nil && s.Clock != nil && s.Enqueue != nil
}

// enqueueDetached schedules the sync job without tying success to the HTTP request lifetime,
// while preserving request_id / correlation from the inbound request context.
func (s *GenerateService) enqueueDetached(parent context.Context, generationJobID, designID string) error {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 15*time.Second)
	defer cancel()
	return s.Enqueue(ctx, generationJobID, designID)
}

// ensureEnqueued re-Enqueues while the product job still needs a delivery.
// Re-reads the job so a concurrent ClaimForWork is not mistaken for still-queued.
// Running re-enqueues only when the DB lease on started_at has expired.
func (s *GenerateService) ensureEnqueued(ctx context.Context, j domain.GenerationJob) error {
	fresh, ok, err := s.Jobs.GetByID(ctx, j.ID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	switch fresh.Status {
	case domain.JobQueued:
		return s.enqueueDetached(ctx, fresh.ID, fresh.DesignID)
	case domain.JobRunning:
		expired, err := s.Jobs.ClaimLeaseExpired(ctx, fresh.ID, domain.DefaultClaimLease)
		if err != nil {
			return err
		}
		if !expired {
			return nil
		}
		return s.enqueueDetached(ctx, fresh.ID, fresh.DesignID)
	default:
		return nil
	}
}

// RequestGenerate creates a GenerationJob and enqueues the sync worker.
func (s *GenerateService) RequestGenerate(ctx context.Context, designID, bearerToken, idempotencyKey, correlationID string) (JobView, error) {
	if !s.depsReady() {
		return JobView{}, &GenerateError{Code: GenerateErrUnavailable, Message: "generate deps missing"}
	}
	key := strings.TrimSpace(idempotencyKey)
	if key == "" || utf8.RuneCountInString(key) > 255 {
		return JobView{}, &GenerateError{Code: GenerateErrBadRequest, Message: "Idempotency-Key required (1-255)"}
	}
	designID = strings.TrimSpace(designID)
	bearerToken = strings.TrimSpace(bearerToken)
	now := s.Clock.Now().UTC()
	if designID == "" || bearerToken == "" {
		return JobView{}, &GenerateError{Code: GenerateErrNotFound, Message: "not found"}
	}

	d, ok, err := s.Designs.GetByID(ctx, designID)
	if err != nil {
		return JobView{}, fmt.Errorf("generate: get design: %w", err)
	}
	if !ok || !OwnerTokenMatches(bearerToken, d.OwnerTokenHash) {
		return JobView{}, &GenerateError{Code: GenerateErrNotFound, Message: "not found"}
	}
	if !d.ExpiresAt.After(now) || d.Status == domain.DesignExpired {
		return JobView{}, &GenerateError{Code: GenerateErrNotFound, Message: "not found"}
	}

	// Preflight source before creating a job.
	source, ok, err := s.Assets.GetByID(ctx, d.SourceAssetID)
	if err != nil {
		return JobView{}, fmt.Errorf("generate: source: %w", err)
	}
	if !ok || source.Type != domain.AssetTypeSource || source.StorageKey == "" {
		return JobView{}, &GenerateError{Code: GenerateErrUploadUnavailable, Message: "source missing"}
	}

	textsDigest := domain.TextsDigestFor(d.Texts)

	// Idempotent replay (scoped to source + template version + texts).
	if existing, found, err := s.Jobs.GetByIdempotency(ctx, designID, key); err != nil {
		return JobView{}, fmt.Errorf("generate: idempotency lookup: %w", err)
	} else if found {
		if !existing.InputMatches(d.SourceAssetID, d.TemplateVersion, textsDigest) {
			return JobView{}, &GenerateError{
				Code: GenerateErrConflict, Message: "idempotency key reused with different inputs",
				JobID: existing.ID, DesignID: designID,
			}
		}
		if err := s.ensureEnqueued(ctx, existing); err != nil {
			return JobView{}, &GenerateError{Code: GenerateErrUnavailable, Message: "enqueue failed", JobID: existing.ID, DesignID: designID}
		}
		return toJobView(existing), nil
	}

	maxAttempts := domain.NormalizeMaxAttempts(s.MaxAttempts)
	provider := strings.TrimSpace(s.Provider)
	if provider == "" {
		provider = "fake"
	}

	// Transition before cooldown so an in-flight generate stays 409, not 429.
	if !d.CanRequestGeneration() {
		if active, found, lookupErr := s.Jobs.GetActiveByDesign(ctx, designID); lookupErr == nil && found {
			return JobView{}, &GenerateError{
				Code: GenerateErrInvalidTransition, Message: (&domain.ErrInvalidTransition{From: d.Status, Command: "RequestGeneration"}).Error(),
				JobID: active.ID, DesignID: designID,
			}
		}
		return JobView{}, &GenerateError{
			Code:     GenerateErrInvalidTransition,
			Message:  (&domain.ErrInvalidTransition{From: d.Status, Command: "RequestGeneration"}).Error(),
			DesignID: designID,
		}
	}
	if err := s.checkCooldown(ctx, d, now); err != nil {
		return JobView{}, err
	}

	jobID := s.IDs.New()
	if err := d.RequestGeneration(jobID, maxAttempts, now); err != nil {
		var lim *domain.ErrLimitReached
		if errors.As(err, &lim) {
			return JobView{}, &GenerateError{Code: GenerateErrLimitReached, Message: lim.Error(), DesignID: designID}
		}
		var it *domain.ErrInvalidTransition
		if errors.As(err, &it) {
			if active, found, lookupErr := s.Jobs.GetActiveByDesign(ctx, designID); lookupErr == nil && found {
				return JobView{}, &GenerateError{
					Code: GenerateErrInvalidTransition, Message: it.Error(),
					JobID: active.ID, DesignID: designID,
				}
			}
			return JobView{}, &GenerateError{Code: GenerateErrInvalidTransition, Message: it.Error(), DesignID: designID}
		}
		return JobView{}, err
	}

	job := domain.NewQueuedJob(jobID, designID, key, provider, d.GenerationAttempts, now, correlationID, d.SourceAssetID, d.TemplateVersion, textsDigest)
	sourceDeleteAfter := now.Add(domain.SourceAttachedRetention)
	if err := s.Jobs.BeginGenerating(ctx, job, d, sourceDeleteAfter); err != nil {
		if errors.Is(err, ports.ErrGenerationJobConflict) {
			if existing, found, lookupErr := s.Jobs.GetByIdempotency(ctx, designID, key); lookupErr != nil {
				return JobView{}, fmt.Errorf("generate: conflict idempotency: %w", lookupErr)
			} else if found {
				if !existing.InputMatches(d.SourceAssetID, d.TemplateVersion, textsDigest) {
					return JobView{}, &GenerateError{
						Code: GenerateErrConflict, Message: "idempotency key reused with different inputs",
						JobID: existing.ID, DesignID: designID,
					}
				}
				if err := s.ensureEnqueued(ctx, existing); err != nil {
					return JobView{}, &GenerateError{Code: GenerateErrUnavailable, Message: "enqueue failed", JobID: existing.ID, DesignID: designID}
				}
				return toJobView(existing), nil
			}
			if active, found, lookupErr := s.Jobs.GetActiveByDesign(ctx, designID); lookupErr != nil {
				return JobView{}, fmt.Errorf("generate: conflict active: %w", lookupErr)
			} else if found {
				return JobView{}, &GenerateError{
					Code: GenerateErrConflict, Message: "active generation job exists",
					JobID: active.ID, DesignID: designID,
				}
			}
			return JobView{}, &GenerateError{Code: GenerateErrConflict, Message: "generation job conflict", DesignID: designID}
		}
		if errors.Is(err, ports.ErrDesignConflict) {
			if active, found, lookupErr := s.Jobs.GetActiveByDesign(ctx, designID); lookupErr == nil && found {
				return JobView{}, &GenerateError{Code: GenerateErrConflict, Message: "design conflict", JobID: active.ID, DesignID: designID}
			}
			return JobView{}, &GenerateError{Code: GenerateErrConflict, Message: "design conflict", DesignID: designID}
		}
		return JobView{}, fmt.Errorf("generate: begin: %w", err)
	}

	if err := s.ensureEnqueued(ctx, job); err != nil {
		// Row is queued — client must retry same Idempotency-Key (ensureEnqueued on replay).
		return JobView{}, &GenerateError{Code: GenerateErrUnavailable, Message: "enqueue failed", JobID: job.ID, DesignID: designID}
	}
	return toJobView(job), nil
}

// GetJob returns a job when the caller owns the parent design.
func (s *GenerateService) GetJob(ctx context.Context, jobID, bearerToken string) (JobView, error) {
	if !s.depsReady() {
		return JobView{}, &GenerateError{Code: GenerateErrUnavailable, Message: "generate deps missing"}
	}
	jobID = strings.TrimSpace(jobID)
	bearerToken = strings.TrimSpace(bearerToken)
	now := s.Clock.Now().UTC()
	if jobID == "" || bearerToken == "" {
		return JobView{}, &GenerateError{Code: GenerateErrNotFound, Message: "not found"}
	}
	job, ok, err := s.Jobs.GetByID(ctx, jobID)
	if err != nil {
		return JobView{}, fmt.Errorf("generate: get job: %w", err)
	}
	if !ok {
		return JobView{}, &GenerateError{Code: GenerateErrNotFound, Message: "not found"}
	}
	d, ok, err := s.Designs.GetByID(ctx, job.DesignID)
	if err != nil {
		return JobView{}, fmt.Errorf("generate: get design: %w", err)
	}
	if !ok || !OwnerTokenMatches(bearerToken, d.OwnerTokenHash) {
		return JobView{}, &GenerateError{Code: GenerateErrNotFound, Message: "not found"}
	}
	if !d.ExpiresAt.After(now) || d.Status == domain.DesignExpired {
		return JobView{}, &GenerateError{Code: GenerateErrNotFound, Message: "not found"}
	}
	return toJobView(job), nil
}

func (s *GenerateService) checkCooldown(ctx context.Context, d domain.Design, now time.Time) error {
	if d.GenerationAttempts == 0 || strings.TrimSpace(d.LatestJobID) == "" {
		return nil
	}
	cooldown := s.Cooldown
	if cooldown <= 0 {
		cooldown = domain.DefaultGenerationCooldown
	}
	job, ok, err := s.Jobs.GetByID(ctx, d.LatestJobID)
	if err != nil {
		return fmt.Errorf("generate: cooldown job: %w", err)
	}
	if !ok {
		return nil
	}
	if err := domain.CheckGenerationCooldown(domain.GenerationJobLastAttemptAt(job), cooldown, now); err != nil {
		var cd *domain.ErrCooldown
		if errors.As(err, &cd) {
			secs := int(cd.RetryAfter.Round(time.Second) / time.Second)
			if secs < 1 {
				secs = 1
			}
			return &GenerateError{
				Code: GenerateErrCooldown, Message: cd.Error(),
				DesignID: d.ID, RetryAfter: secs,
			}
		}
		return err
	}
	return nil
}

func toJobView(j domain.GenerationJob) JobView {
	return JobView{
		ID:             j.ID,
		DesignID:       j.DesignID,
		Status:         j.Status,
		Attempt:        j.Attempt,
		ErrorCode:      j.ErrorCode,
		TerminalReason: j.TerminalReason,
		RetryAfter:     ports.RetryAfterForStatus(j.Status),
	}
}
