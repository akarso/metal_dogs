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

// DeletedBlobRef is a soft-deleted asset whose object the caller should remove.
type DeletedBlobRef struct {
	AssetID    string
	StorageKey string
}

// GenerationJobRepository persists GenerationJob aggregates.
type GenerationJobRepository interface {
	Insert(ctx context.Context, j domain.GenerationJob) error
	GetByID(ctx context.Context, id string) (domain.GenerationJob, bool, error)
	GetByIdempotency(ctx context.Context, designID, key string) (domain.GenerationJob, bool, error)
	GetActiveByDesign(ctx context.Context, designID string) (domain.GenerationJob, bool, error)
	// BeginGenerating inserts the job, marks design generating, and slides source delete_after.
	// expectedAttempts / expectedLatestJobID are the design values before RequestGeneration
	// (optimistic fence against a concurrent regenerate that already committed).
	BeginGenerating(ctx context.Context, j domain.GenerationJob, d domain.Design, sourceDeleteAfter time.Time, expectedAttempts int, expectedLatestJobID string) error
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
	// PersistArtwork soft-deletes prior live artwork for the design, inserts artwork, and sets
	// job.artwork_asset_id while the job stays active (claim-token fenced). Returns soft-deleted
	// prior artwork refs for caller blob cleanup. Enables resume without another provider call.
	PersistArtwork(ctx context.Context, j domain.GenerationJob, artwork domain.Asset) ([]DeletedBlobRef, error)
	// CompleteSuccess soft-deletes prior mockup, inserts mockup (required), succeeds job + design.
	// Artwork must already be on the job via PersistArtwork. Returns soft-deleted mockup refs.
	CompleteSuccess(ctx context.Context, j domain.GenerationJob, mockup domain.Asset, design domain.Design) ([]DeletedBlobRef, error)
	// FinalizeSuccess updates an active job to succeeded + design preview_ready.
	// When mockup is non-zero it soft-deletes prior mockup and inserts the new row.
	FinalizeSuccess(ctx context.Context, j domain.GenerationJob, design domain.Design, mockup domain.Asset) ([]DeletedBlobRef, error)
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
