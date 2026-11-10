package ports

import (
	"context"
	"errors"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// ErrGenerationJobConflict means an insert hit the active-job or idempotency unique index.
var ErrGenerationJobConflict = errors.New("generation job conflict")

// ErrGenerationJobStale means a conditional status/token update matched zero rows.
var ErrGenerationJobStale = errors.New("generation job stale")

// ErrGenerationJobHasArtwork means FailActive refused to fail a row that already has artwork.
var ErrGenerationJobHasArtwork = errors.New("generation job has artwork")

// GenerationJobRepository persists GenerationJob aggregates.
type GenerationJobRepository interface {
	Insert(ctx context.Context, j domain.GenerationJob) error
	GetByID(ctx context.Context, id string) (domain.GenerationJob, bool, error)
	GetByIdempotency(ctx context.Context, designID, key string) (domain.GenerationJob, bool, error)
	GetActiveByDesign(ctx context.Context, designID string) (domain.GenerationJob, bool, error)
	// BeginGenerating inserts the job, marks design generating, and slides source delete_after.
	BeginGenerating(ctx context.Context, j domain.GenerationJob, d domain.Design, sourceDeleteAfter time.Time) error
	// ClaimForWork atomically transitions queued/validating → running, or reclaim running past lease.
	// Sets started_at from the database clock and bumps claim_token. Returns ErrGenerationJobStale
	// when another delivery holds the claim.
	ClaimForWork(ctx context.Context, id string, lease time.Duration) (domain.GenerationJob, error)
	// ClaimLeaseExpired reports whether a running claim is past lease using database NOW().
	ClaimLeaseExpired(ctx context.Context, id string, lease time.Duration) (bool, error)
	// InvalidateClaim bumps claim_token and clears started_at so an in-flight handler cannot
	// write and the next ClaimForWork can take the row after a platform Requeue.
	InvalidateClaim(ctx context.Context, id string) error
	// Update writes job fields when status still equals expectedStatus and claim_token matches.
	Update(ctx context.Context, j domain.GenerationJob, expectedStatus string) error
	// CompleteSuccess soft-deletes prior artwork for the design, inserts artwork, succeeds job + design.
	CompleteSuccess(ctx context.Context, j domain.GenerationJob, artwork domain.Asset, design domain.Design) error
	// FinalizeSuccess updates job succeeded + design preview_ready without inserting an asset.
	FinalizeSuccess(ctx context.Context, j domain.GenerationJob, design domain.Design) error
	// CompleteFailure marks an active job failed + design generation_failed (token-fenced).
	CompleteFailure(ctx context.Context, j domain.GenerationJob, design domain.Design) error
	// FailActive marks an active job failed without wiping provider/model/artwork fields.
	// claimToken must match the row; returns ErrGenerationJobStale on mismatch.
	// Returns ErrGenerationJobHasArtwork when the row already has artwork_asset_id set.
	FailActive(ctx context.Context, id, designID, terminalReason, errorCode string, claimToken int64, at time.Time) error
}

// GenerationEnqueue enqueues integration.sync.metaldogs.generate after commit.
type GenerationEnqueue func(ctx context.Context, generationJobID, designID string) error

// DefaultRetryAfterSeconds is suggested poll interval while the job is active.
const DefaultRetryAfterSeconds = 2

// RetryAfterForStatus returns seconds clients should wait before the next poll.
func RetryAfterForStatus(status string) int {
	switch status {
	case domain.JobQueued, domain.JobRunning, domain.JobValidating:
		return DefaultRetryAfterSeconds
	default:
		return 0
	}
}
