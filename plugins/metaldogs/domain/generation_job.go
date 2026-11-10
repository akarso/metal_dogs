package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// GenerationJob status values (ADR-0006).
const (
	JobQueued     = "queued"
	JobRunning    = "running"
	JobValidating = "validating"
	JobSucceeded  = "succeeded"
	JobFailed     = "failed"
	JobCancelled  = "cancelled"
)

// GenerationJobStatuses is the golden list for docs/tests.
var GenerationJobStatuses = []string{
	JobQueued, JobRunning, JobValidating, JobSucceeded, JobFailed, JobCancelled,
}

// Terminal reasons (stable; mapped for UI).
const (
	TerminalProviderTimeout       = "provider_timeout"
	TerminalProviderRejectedInput = "provider_rejected_input"
	TerminalProviderRateLimited   = "provider_rate_limited"
	TerminalProviderError         = "provider_error"
	TerminalValidationFailed      = "validation_failed"
	TerminalStorageError          = "storage_error"
	TerminalAttemptsExhausted     = "attempts_exhausted"
	TerminalCancelled             = "cancelled"
	TerminalCostCapReached        = "cost_cap_reached"
	TerminalProviderResultLost    = "provider_result_lost"
)

// ErrLimitReached means generation_attempts would exceed the configured cap.
type ErrLimitReached struct {
	Attempts int
	Max      int
}

func (e *ErrLimitReached) Error() string {
	return fmt.Sprintf("limit_reached: attempts %d >= max %d", e.Attempts, e.Max)
}

// GenerationJob is one generation attempt for a design.
type GenerationJob struct {
	ID              string
	DesignID        string
	Attempt         int
	Status          string
	IdempotencyKey  string
	Provider        string
	Model           string
	PromptVersion   string
	ParametersJSON  []byte // raw jsonb; empty means {}
	ArtworkAssetID  string
	MockupAssetID   string
	ValidationJSON  []byte
	ErrorCode       string
	TerminalReason  string
	Cost            *float64
	CostCurrency    string
	LatencyMS       *int
	CorrelationID   string
	SourceAssetID   string // input scope for idempotency replay
	TemplateVersion int
	TextsDigest     string // sha256 hex of canonical design texts
	ClaimToken      int64  // bumped on each successful claim / invalidate
	QueuedAt        time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
}

// ErrClaimHeld means another delivery still holds the running lease.
var ErrClaimHeld = errors.New("generation job claim held")

// DefaultClaimLease is how long a running claim blocks peer re-entry.
const DefaultClaimLease = 2 * time.Minute

// TextsDigestFor returns a stable sha256 hex of sorted design texts (empty map → empty string).
func TextsDigestFor(texts map[string]string) string {
	if len(texts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(texts))
	for k := range texts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]string, len(keys))
	for _, k := range keys {
		ordered[k] = texts[k]
	}
	raw, err := json.Marshal(ordered)
	if err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%v", texts)))
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// NewQueuedJob builds a queued job row (attempt already assigned by caller).
func NewQueuedJob(id, designID, idempotencyKey, provider string, attempt int, at time.Time, correlationID, sourceAssetID string, templateVersion int, textsDigest string) GenerationJob {
	return GenerationJob{
		ID:              id,
		DesignID:        designID,
		Attempt:         attempt,
		Status:          JobQueued,
		IdempotencyKey:  strings.TrimSpace(idempotencyKey),
		Provider:        provider,
		QueuedAt:        at.UTC(),
		CorrelationID:   strings.TrimSpace(correlationID),
		SourceAssetID:   strings.TrimSpace(sourceAssetID),
		TemplateVersion: templateVersion,
		TextsDigest:     strings.TrimSpace(textsDigest),
	}
}

// ClaimForWork moves queued/validating → running. A running row is reclaimable
// only when started_at is missing or older than lease (stage timeout).
// at must be the same clock as started_at (in-memory tests); Postgres uses NOW().
func (j *GenerationJob) ClaimForWork(at time.Time, lease time.Duration) error {
	if lease <= 0 {
		lease = DefaultClaimLease
	}
	now := at.UTC()
	switch j.Status {
	case JobQueued, JobValidating:
		// ok
	case JobRunning:
		if j.StartedAt != nil && now.Sub(*j.StartedAt) < lease {
			return ErrClaimHeld
		}
	default:
		return &ErrInvalidTransition{From: j.Status, Command: "ClaimForWork"}
	}
	j.Status = JobRunning
	j.StartedAt = &now
	j.ClaimToken++
	return nil
}

// LeaseExpired reports whether a running claim is past lease using the same
// reference time that started_at was written with (DB NOW() in production).
func (j GenerationJob) LeaseExpired(now time.Time, lease time.Duration) bool {
	if j.Status != JobRunning {
		return false
	}
	if lease <= 0 {
		lease = DefaultClaimLease
	}
	if j.StartedAt == nil {
		return true
	}
	return !now.Before(j.StartedAt.Add(lease))
}

// MarkValidating transitions running → validating (in-memory stage marker).
func (j *GenerationJob) MarkValidating() error {
	if j.Status != JobRunning {
		return &ErrInvalidTransition{From: j.Status, Command: "MarkValidating"}
	}
	j.Status = JobValidating
	return nil
}

// RequeueForRetry returns running/validating → queued after a transient failure.
func (j *GenerationJob) RequeueForRetry() error {
	switch j.Status {
	case JobRunning, JobValidating:
		j.Status = JobQueued
		j.StartedAt = nil
		return nil
	default:
		return &ErrInvalidTransition{From: j.Status, Command: "RequeueForRetry"}
	}
}

// Succeed marks the job succeeded with artwork and mockup preview assets.
func (j *GenerationJob) Succeed(artworkAssetID, mockupAssetID string, validationJSON []byte, latencyMS int, cost *float64, currency, model, promptVersion string, parametersJSON []byte, at time.Time) error {
	if j.Status != JobValidating && j.Status != JobRunning {
		return &ErrInvalidTransition{From: j.Status, Command: "Succeed"}
	}
	if strings.TrimSpace(artworkAssetID) == "" {
		return fmt.Errorf("artwork asset id required")
	}
	if strings.TrimSpace(mockupAssetID) == "" {
		return fmt.Errorf("mockup asset id required")
	}
	now := at.UTC()
	j.Status = JobSucceeded
	j.ArtworkAssetID = artworkAssetID
	j.MockupAssetID = strings.TrimSpace(mockupAssetID)
	j.ValidationJSON = validationJSON
	j.LatencyMS = &latencyMS
	j.Cost = cost
	j.CostCurrency = currency
	j.Model = model
	j.PromptVersion = promptVersion
	if len(parametersJSON) > 0 {
		j.ParametersJSON = parametersJSON
	}
	j.FinishedAt = &now
	j.ErrorCode = ""
	j.TerminalReason = ""
	return nil
}

// Fail marks the job failed with a terminal reason.
func (j *GenerationJob) Fail(terminalReason, errorCode string, at time.Time) error {
	switch j.Status {
	case JobQueued, JobRunning, JobValidating:
		// ok
	default:
		return &ErrInvalidTransition{From: j.Status, Command: "Fail"}
	}
	now := at.UTC()
	j.Status = JobFailed
	j.TerminalReason = strings.TrimSpace(terminalReason)
	j.ErrorCode = strings.TrimSpace(errorCode)
	j.FinishedAt = &now
	return nil
}

// ForceFail sets failed regardless of current status (compensating path after a rolled-back Succeed).
// Clears MockupAssetID when the mockup object was deleted and never inserted. Keeps ArtworkAssetID
// so a previously PersistArtwork'd row is not wiped by CompleteFailure.
func (j *GenerationJob) ForceFail(terminalReason, errorCode string, at time.Time) {
	now := at.UTC()
	j.Status = JobFailed
	j.TerminalReason = strings.TrimSpace(terminalReason)
	j.ErrorCode = strings.TrimSpace(errorCode)
	j.FinishedAt = &now
	j.MockupAssetID = ""
}

// IsActive reports queued/running/validating.
func (j GenerationJob) IsActive() bool {
	switch j.Status {
	case JobQueued, JobRunning, JobValidating:
		return true
	default:
		return false
	}
}

// IsTerminal reports succeeded/failed/cancelled.
func (j GenerationJob) IsTerminal() bool {
	switch j.Status {
	case JobSucceeded, JobFailed, JobCancelled:
		return true
	default:
		return false
	}
}

// InputMatches reports whether this job was created for the same source, template version, and texts.
func (j GenerationJob) InputMatches(sourceAssetID string, templateVersion int, textsDigest string) bool {
	return j.SourceAssetID == strings.TrimSpace(sourceAssetID) &&
		j.TemplateVersion == templateVersion &&
		j.TextsDigest == strings.TrimSpace(textsDigest)
}
