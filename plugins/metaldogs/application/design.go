package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// DesignError is a classified design failure for HTTP mapping.
type DesignError struct {
	Code      string
	Message   string
	VersionID string     // set on approve enqueue/view 503 so clients can retry
	DesignID  string
	JobID     string     // set when a generation job lookup/preview I/O fails
	Design    DesignView // set when a mutation committed but the response view failed
	Cause     error      // optional wrapped infra cause (logged; not serialized)
}

func (e *DesignError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = e.Code
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", msg, e.Cause)
	}
	return msg
}

func (e *DesignError) Unwrap() error { return e.Cause }

// Stable design error codes.
const (
	DesignErrUnavailable           = "unavailable"
	DesignErrNotFound              = "not_found"
	DesignErrBadRequest            = "bad_request"
	DesignErrTemplateUnavailable   = "template_unavailable"
	DesignErrUploadUnavailable     = "upload_unavailable"
	DesignErrUploadAlreadyAttached = "upload_already_attached"
	DesignErrInvalidTexts          = "invalid_texts"
	DesignErrInvalidTransition     = "invalid_transition"
	DesignErrConflict              = "conflict" // lost approve race that still approved (retry → idempotent)
	DesignErrTooManyRequests       = "too_many_requests"
	DesignErrChecksumMismatch      = "checksum_mismatch"
	DesignErrPayloadTooLarge       = "payload_too_large"
)

// DesignView is the customer-facing design projection (no token hash).
type DesignView struct {
	ID                          string
	Status                      string
	TemplateID                  string
	TemplateVersion             int
	Texts                       map[string]string
	UploadID                    string
	CreatedAt                   time.Time
	ExpiresAt                   time.Time
	SourcePreviewURL            string
	SourcePreviewURLExpiresAt   time.Time // zero when preview unavailable
	ArtworkPreviewURL           string
	ArtworkPreviewURLExpiresAt  time.Time
	MockupPreviewURL            string
	MockupPreviewURLExpiresAt   time.Time
	ApprovedVersionID           string
	OwnerToken                  string // set only on Create / Clone
	GenerationAttempts          int
	GenerationAttemptsRemaining int
	MaxGenerationAttempts       int
	RetryAfter                  int // seconds until regenerate is allowed (0 = now)
}

// DesignService coordinates design create/get/patch/approve/clone with ownership tokens.
type DesignService struct {
	Designs   ports.DesignRepository
	Assets    ports.AssetRepository
	Jobs      ports.GenerationJobRepository // optional; used for artwork/mockup previews (C5) + approve
	Versions  ports.DesignVersionRepository // optional until C6; required for Approve
	Templates *TemplateCatalog
	Store     ports.AssetStore
	IDs       ports.IDGenerator
	Clock     ports.Clock
	// MaxAttempts is plugins.metaldogs.generation.max_attempts_per_design (C7 remaining count).
	MaxAttempts int
	// Cooldown is plugins.metaldogs.generation.cooldown_seconds (0 disables).
	Cooldown time.Duration
	// EnqueuePrepare schedules prepare_production_artwork after approve (optional).
	EnqueuePrepare ports.PrepareProductionEnqueue
	// MaxInFlightClones caps concurrent Clone calls; 0 → DefaultMaxConcurrentUploads.
	MaxInFlightClones int
	// OnCleanupError is optional; op is "delete", "soft_delete", or "clear_storage_key"; never pass URLs or tokens.
	OnCleanupError func(op, assetID, storageKey string, err error)
	// OnPrepareEnqueueError is optional; ids only — no tokens/URLs.
	OnPrepareEnqueueError func(designID, versionID string, err error)

	cloneOnce     sync.Once
	cloneInFlight chan struct{}
}

func (s *DesignService) acquireClone() bool {
	s.cloneOnce.Do(func() {
		n := s.MaxInFlightClones
		if n <= 0 {
			n = DefaultMaxConcurrentUploads
		}
		s.cloneInFlight = make(chan struct{}, n)
	})
	select {
	case s.cloneInFlight <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *DesignService) releaseClone() {
	select {
	case <-s.cloneInFlight:
	default:
	}
}

// CreateInput is the public create payload.
type CreateInput struct {
	TemplateID string
	UploadID   string
	Texts      map[string]string
}

// PatchInput is the public patch payload (nil maps mean "omit").
type PatchInput struct {
	Texts    map[string]string // nil = leave texts unchanged; empty map invalid via validation
	UploadID *string           // nil = leave photo; non-nil replaces
}

func (s *DesignService) depsReady() bool {
	return s != nil && s.Designs != nil && s.Assets != nil && s.Templates != nil &&
		s.Store != nil && s.IDs != nil && s.Clock != nil
}

func (s *DesignService) cleanupErr(op, assetID, storageKey string, err error) {
	if err == nil || s.OnCleanupError == nil {
		return
	}
	s.OnCleanupError(op, assetID, storageKey, err)
}

// Create builds a draft design, binds the upload, and returns the owner token once.
// Preview signing soft-fails: the token is always returned after CreateBound commits.
func (s *DesignService) Create(ctx context.Context, in CreateInput) (DesignView, error) {
	if !s.depsReady() {
		return DesignView{}, &DesignError{Code: DesignErrUnavailable, Message: "design deps missing"}
	}
	templateID := strings.TrimSpace(in.TemplateID)
	uploadID := strings.TrimSpace(in.UploadID)
	if templateID == "" || uploadID == "" {
		return DesignView{}, &DesignError{Code: DesignErrBadRequest, Message: "template_id and upload_id required"}
	}

	tpl, ok, err := s.Templates.GetPublishedByID(ctx, templateID)
	if err != nil {
		return DesignView{}, fmt.Errorf("design: template: %w", err)
	}
	if !ok {
		return DesignView{}, &DesignError{Code: DesignErrTemplateUnavailable, Message: "template not published"}
	}

	texts, err := validateTextsFromPublished(tpl, in.Texts)
	if err != nil {
		return DesignView{}, err
	}

	asset, ok, err := s.Assets.GetByID(ctx, uploadID)
	if err != nil {
		return DesignView{}, fmt.Errorf("design: asset: %w", err)
	}
	if !ok || asset.Type != domain.AssetTypeSource {
		return DesignView{}, &DesignError{Code: DesignErrUploadUnavailable, Message: "upload missing or not a source"}
	}
	if asset.OwnerRef != "" {
		return DesignView{}, &DesignError{Code: DesignErrUploadAlreadyAttached, Message: "upload already attached"}
	}

	token, hash, err := MintOwnerToken()
	if err != nil {
		return DesignView{}, fmt.Errorf("design: mint token: %w", err)
	}
	now := s.Clock.Now().UTC()
	id := s.IDs.New()
	assetDeleteAfter := now.Add(domain.SourceAttachedRetention)
	d := domain.Design{
		ID:              id,
		TemplateID:      tpl.ID,
		TemplateVersion: tpl.Version,
		Status:          domain.DesignDraft,
		Texts:           texts,
		SourceAssetID:   uploadID,
		OwnerTokenHash:  hash,
		CreatedAt:       now,
		UpdatedAt:       now,
		ExpiresAt:       now.Add(domain.DesignRetentionUnordered),
	}
	if err := s.Designs.CreateBound(ctx, d, assetDeleteAfter); err != nil {
		if errors.Is(err, ports.ErrAssetBindConflict) {
			return DesignView{}, &DesignError{Code: DesignErrUploadAlreadyAttached, Message: "upload already attached"}
		}
		return DesignView{}, fmt.Errorf("design: create: %w", err)
	}
	// After commit the plaintext token must still reach the client even if preview lookup fails.
	view := s.createCloneView(ctx, d, now)
	view.OwnerToken = token
	return view, nil
}

// Get returns a design when the bearer token matches and the design has not expired.
// GET is read-only: design expires_at is not slid by reads (retention table: 30d from create /
// last mutating activity).
func (s *DesignService) Get(ctx context.Context, id, bearerToken string) (DesignView, error) {
	d, now, err := s.loadAuthorized(ctx, id, bearerToken)
	if err != nil {
		return DesignView{}, err
	}
	return s.toView(ctx, d, now)
}

// Patch updates texts and/or photo when authorized and editable.
func (s *DesignService) Patch(ctx context.Context, id, bearerToken string, in PatchInput) (DesignView, error) {
	d, now, err := s.loadAuthorized(ctx, id, bearerToken)
	if err != nil {
		return DesignView{}, err
	}
	expectedUpdatedAt := d.UpdatedAt
	oldAsset := d.SourceAssetID
	changedPhoto := false
	assetDeleteAfter := now.Add(domain.SourceAttachedRetention)
	// Mutating activity slides design retention (not GET).
	d.ExpiresAt = now.Add(domain.DesignRetentionUnordered)

	if in.Texts != nil {
		tpl, ok, err := s.Templates.GetPinnedByID(ctx, d.TemplateID)
		if err != nil {
			return DesignView{}, fmt.Errorf("design: template: %w", err)
		}
		if !ok {
			return DesignView{}, &DesignError{Code: DesignErrTemplateUnavailable, Message: "pinned template missing"}
		}
		texts, err := validateTextsFromPublished(tpl, in.Texts)
		if err != nil {
			return DesignView{}, err
		}
		if err := d.ApplyTexts(texts, now); err != nil {
			return DesignView{}, mapTransition(err)
		}
	}
	if in.UploadID != nil {
		uploadID := strings.TrimSpace(*in.UploadID)
		if uploadID == "" {
			return DesignView{}, &DesignError{Code: DesignErrBadRequest, Message: "upload_id required when provided"}
		}
		if uploadID != d.SourceAssetID {
			asset, ok, err := s.Assets.GetByID(ctx, uploadID)
			if err != nil {
				return DesignView{}, fmt.Errorf("design: asset: %w", err)
			}
			if !ok || asset.Type != domain.AssetTypeSource {
				return DesignView{}, &DesignError{Code: DesignErrUploadUnavailable, Message: "upload missing or not a source"}
			}
			if asset.OwnerRef != "" && asset.OwnerRef != d.ID {
				return DesignView{}, &DesignError{Code: DesignErrUploadAlreadyAttached, Message: "upload already attached"}
			}
			if err := d.ApplyPhoto(uploadID, now); err != nil {
				return DesignView{}, mapTransition(err)
			}
			changedPhoto = true
		}
	}
	if in.Texts == nil && in.UploadID == nil {
		return DesignView{}, &DesignError{Code: DesignErrBadRequest, Message: "texts or upload_id required"}
	}
	if in.Texts == nil && in.UploadID != nil && !changedPhoto {
		if err := d.AssertEditable("ReplacePhoto"); err != nil {
			return DesignView{}, mapTransition(err)
		}
		d.UpdatedAt = now
	}

	// Cooldown/latest-job fail-closed before mutating so clients do not see a write
	// succeed then a 503 with no body (especially after photo replace deletes the old blob).
	_, remaining, _ := s.attemptBudget(d)
	retryAfter, err := s.regenerateRetryAfter(ctx, d, remaining, now)
	if err != nil {
		return DesignView{}, err
	}

	if changedPhoto {
		oldKey, err := s.Designs.ReplaceSource(ctx, d, oldAsset, expectedUpdatedAt, assetDeleteAfter)
		if err != nil {
			if errors.Is(err, ports.ErrAssetBindConflict) {
				return DesignView{}, &DesignError{Code: DesignErrUploadAlreadyAttached, Message: "upload already attached"}
			}
			if errors.Is(err, ports.ErrDesignConflict) {
				return DesignView{}, &DesignError{Code: DesignErrConflict, Message: "design conflict"}
			}
			return DesignView{}, fmt.Errorf("design: replace source: %w", err)
		}
		s.removeReplacedBlob(ctx, oldAsset, oldKey)
	} else {
		if err := s.Designs.UpdateEditable(ctx, d, oldAsset, expectedUpdatedAt, assetDeleteAfter); err != nil {
			if errors.Is(err, ports.ErrDesignConflict) {
				return DesignView{}, &DesignError{Code: DesignErrConflict, Message: "design conflict"}
			}
			return DesignView{}, fmt.Errorf("design: update: %w", err)
		}
	}
	// Reuse preflight retry_after — do not re-read the job for cooldown (avoids commit-then-503
	// on a blip, and avoids advertising a stale preflight retry_after after a failed re-read).
	view, err := s.toViewWithRetryAfter(ctx, d, now, retryAfter)
	if err != nil {
		return DesignView{}, s.patchViewAfterCommitError(ctx, d, now, retryAfter, err)
	}
	return view, nil
}

// patchViewAfterCommitError attaches the committed design (with preflight retry_after)
// so clients do not retry a photo replace whose prior blob was already deleted.
// Cooldown failures never reach here — they return before UpdateEditable/ReplaceSource.
func (s *DesignService) patchViewAfterCommitError(ctx context.Context, d domain.Design, now time.Time, retryAfter int, cause error) error {
	committed := s.budgetView(d, retryAfter)
	s.signPreviewsSoft(ctx, &committed, d, now)
	var de *DesignError
	if errors.As(cause, &de) {
		out := *de
		out.Design = committed
		if out.DesignID == "" {
			out.DesignID = d.ID
		}
		return &out
	}
	return &DesignError{
		Code: DesignErrUnavailable, Message: "design updated; view unavailable",
		DesignID: d.ID, Design: committed, Cause: cause,
	}
}

func (s *DesignService) removeReplacedBlob(ctx context.Context, assetID, storageKey string) {
	if storageKey == "" {
		return
	}
	delErr := s.Store.Delete(ctx, storageKey)
	if delErr != nil && !errors.Is(delErr, os.ErrNotExist) {
		s.cleanupErr("delete", assetID, storageKey, delErr)
		return
	}
	if clearErr := s.Assets.ClearStorageKey(ctx, assetID); clearErr != nil {
		s.cleanupErr("clear_storage_key", assetID, storageKey, clearErr)
	}
}

func (s *DesignService) loadAuthorized(ctx context.Context, id, bearerToken string) (domain.Design, time.Time, error) {
	if !s.depsReady() {
		return domain.Design{}, time.Time{}, &DesignError{Code: DesignErrUnavailable, Message: "design deps missing"}
	}
	id = strings.TrimSpace(id)
	bearerToken = strings.TrimSpace(bearerToken)
	now := s.Clock.Now().UTC()
	if id == "" || bearerToken == "" {
		return domain.Design{}, now, &DesignError{Code: DesignErrNotFound, Message: "not found"}
	}
	d, ok, err := s.Designs.GetByID(ctx, id)
	if err != nil {
		return domain.Design{}, now, fmt.Errorf("design: get: %w", err)
	}
	if !ok || !OwnerTokenMatches(bearerToken, d.OwnerTokenHash) {
		return domain.Design{}, now, &DesignError{Code: DesignErrNotFound, Message: "not found"}
	}
	if !d.ExpiresAt.After(now) || d.Status == domain.DesignExpired {
		return domain.Design{}, now, &DesignError{Code: DesignErrNotFound, Message: "not found"}
	}
	return d, now, nil
}

func (s *DesignService) attemptBudget(d domain.Design) (attempts, remaining, max int) {
	max = domain.NormalizeMaxAttempts(s.MaxAttempts)
	attempts = d.GenerationAttempts
	if attempts < 0 {
		attempts = 0
	}
	return attempts, domain.GenerationAttemptsRemaining(attempts, max), max
}

// regenerateRetryAfter mirrors POST generate cooldown rules for the design read model.
// retry_after: 0 means "not locked by cooldown" — callers must still check status/remaining.
// Missing/unreadable latest job with attempts>0 and cooldown on → unavailable (same as POST).
func (s *DesignService) regenerateRetryAfter(ctx context.Context, d domain.Design, remaining int, now time.Time) (int, error) {
	if remaining <= 0 || d.GenerationAttempts == 0 || s.Cooldown <= 0 {
		return 0, nil
	}
	if s.Jobs == nil {
		return 0, &DesignError{Code: DesignErrUnavailable, Message: "latest generation job missing", DesignID: d.ID}
	}
	latestID := strings.TrimSpace(d.LatestJobID)
	if latestID == "" {
		return 0, &DesignError{Code: DesignErrUnavailable, Message: "latest generation job missing", DesignID: d.ID}
	}
	job, ok, err := s.Jobs.GetByID(ctx, latestID)
	if err != nil {
		return 0, &DesignError{
			Code: DesignErrUnavailable, Message: "latest generation job unreadable",
			DesignID: d.ID, JobID: latestID,
			Cause: fmt.Errorf("design: cooldown job: %w", err),
		}
	}
	if !ok || job.DesignID != d.ID {
		return 0, &DesignError{
			Code: DesignErrUnavailable, Message: "latest generation job missing",
			DesignID: d.ID, JobID: latestID,
		}
	}
	if job.IsActive() {
		return domain.CooldownRetryAfterSeconds(s.Cooldown, s.Cooldown), nil
	}
	anchor, ok := domain.GenerationJobCooldownAnchor(job)
	if !ok {
		return 0, &DesignError{
			Code: DesignErrUnavailable, Message: "latest generation job missing finished_at",
			DesignID: d.ID, JobID: latestID,
		}
	}
	if err := domain.CheckGenerationCooldown(anchor, s.Cooldown, now); err != nil {
		var cd *domain.ErrCooldown
		if errors.As(err, &cd) {
			return domain.CooldownRetryAfterSeconds(cd.RetryAfter, s.Cooldown), nil
		}
		return 0, err
	}
	return 0, nil
}

func (s *DesignService) budgetView(d domain.Design, retryAfter int) DesignView {
	attempts, remaining, max := s.attemptBudget(d)
	return DesignView{
		ID: d.ID, Status: d.Status, TemplateID: d.TemplateID, TemplateVersion: d.TemplateVersion,
		Texts: d.Texts, UploadID: d.SourceAssetID, CreatedAt: d.CreatedAt, ExpiresAt: d.ExpiresAt,
		ApprovedVersionID:           d.ApprovedVersionID,
		GenerationAttempts:          attempts,
		GenerationAttemptsRemaining: remaining,
		MaxGenerationAttempts:       max,
		RetryAfter:                  retryAfter,
	}
}

// createCloneView is for create/clone after the write committed. Attempts are always 0,
// so cooldown never applies; preview signing soft-fails so owner_token still returns.
func (s *DesignService) createCloneView(ctx context.Context, d domain.Design, now time.Time) DesignView {
	view := s.budgetView(d, 0)
	s.signPreviewsSoft(ctx, &view, d, now)
	return view
}

// approveViewAfterCommit matches GET fail-closed rules for cooldown and preview job/asset
// I/O (including when cooldown_seconds is 0 and the cooldown read is skipped).
func (s *DesignService) approveViewAfterCommit(ctx context.Context, d domain.Design, now time.Time) (DesignView, error) {
	_, remaining, _ := s.attemptBudget(d)
	retryAfter, err := s.regenerateRetryAfter(ctx, d, remaining, now)
	if err != nil {
		return DesignView{}, err
	}
	return s.toViewWithRetryAfter(ctx, d, now, retryAfter)
}

// toView returns lookup/cooldown errors to GET/PATCH callers.
// A missing/soft-deleted source omits UploadID; live rows keep UploadID on signer failure.
func (s *DesignService) toView(ctx context.Context, d domain.Design, now time.Time) (DesignView, error) {
	_, remaining, _ := s.attemptBudget(d)
	retryAfter, err := s.regenerateRetryAfter(ctx, d, remaining, now)
	if err != nil {
		return DesignView{}, err
	}
	return s.toViewWithRetryAfter(ctx, d, now, retryAfter)
}

// toViewWithRetryAfter builds the read model using a cooldown value already computed
// (PATCH preflight) so the latest job is not read twice for cooldown.
func (s *DesignService) toViewWithRetryAfter(ctx context.Context, d domain.Design, now time.Time, retryAfter int) (DesignView, error) {
	view := s.budgetView(d, retryAfter)
	if err := s.signPreviewsHard(ctx, &view, d, now); err != nil {
		return DesignView{}, err
	}
	return view, nil
}

func (s *DesignService) signPreviewsSoft(ctx context.Context, view *DesignView, d domain.Design, now time.Time) {
	_ = s.signPreviews(ctx, view, d, now, true)
}

func (s *DesignService) signPreviewsHard(ctx context.Context, view *DesignView, d domain.Design, now time.Time) error {
	return s.signPreviews(ctx, view, d, now, false)
}

func (s *DesignService) signPreviews(ctx context.Context, view *DesignView, d domain.Design, now time.Time, soft bool) error {
	if s.Assets == nil || s.Store == nil {
		return nil
	}
	asset, ok, err := s.Assets.GetByID(ctx, d.SourceAssetID)
	if err != nil {
		if soft {
			return nil
		}
		return fmt.Errorf("design: source asset: %w", err)
	}
	if !ok {
		view.UploadID = ""
	} else if asset.StorageKey != "" {
		if url, signErr := s.Store.SignedGetURL(ctx, asset.StorageKey, ports.DefaultSignedGetTTL); signErr == nil {
			view.SourcePreviewURL = url
			view.SourcePreviewURLExpiresAt = now.Add(ports.DefaultSignedGetTTL)
		}
	}
	if err := s.attachGenerationPreviews(ctx, view, d, now); err != nil {
		if soft {
			return nil
		}
		return err
	}
	return nil
}

func (s *DesignService) attachGenerationPreviews(ctx context.Context, view *DesignView, d domain.Design, now time.Time) error {
	if s.Jobs == nil || s.Assets == nil || s.Store == nil || d.LatestJobID == "" {
		return nil
	}
	switch d.Status {
	case domain.DesignPreviewReady, domain.DesignApproved, domain.DesignOrdered:
		// ok
	default:
		return nil
	}
	jobID := d.LatestJobID
	job, ok, err := s.Jobs.GetByID(ctx, jobID)
	if err != nil {
		return &DesignError{
			Code: DesignErrUnavailable, Message: "generation preview job unreadable",
			DesignID: d.ID, JobID: jobID,
			Cause: fmt.Errorf("design: generation job: %w", err),
		}
	}
	if !ok || job.Status != domain.JobSucceeded || job.DesignID != d.ID {
		return nil
	}
	expires := now.Add(ports.DefaultSignedGetTTL)
	if job.ArtworkAssetID != "" {
		a, aok, aerr := s.Assets.GetByID(ctx, job.ArtworkAssetID)
		if aerr != nil {
			return &DesignError{
				Code: DesignErrUnavailable, Message: "generation preview artwork unreadable",
				DesignID: d.ID, JobID: jobID,
				Cause: fmt.Errorf("design: artwork asset: %w", aerr),
			}
		}
		if aok && a.Type == domain.AssetTypeArtwork && a.OwnerRef == d.ID && a.StorageKey != "" {
			if u, signErr := s.Store.SignedGetURL(ctx, a.StorageKey, ports.DefaultSignedGetTTL); signErr == nil {
				view.ArtworkPreviewURL = u
				view.ArtworkPreviewURLExpiresAt = expires
			}
		}
	}
	if job.MockupAssetID != "" {
		a, aok, aerr := s.Assets.GetByID(ctx, job.MockupAssetID)
		if aerr != nil {
			return &DesignError{
				Code: DesignErrUnavailable, Message: "generation preview mockup unreadable",
				DesignID: d.ID, JobID: jobID,
				Cause: fmt.Errorf("design: mockup asset: %w", aerr),
			}
		}
		if aok && a.Type == domain.AssetTypeMockup && a.OwnerRef == d.ID && a.StorageKey != "" {
			if u, signErr := s.Store.SignedGetURL(ctx, a.StorageKey, ports.DefaultSignedGetTTL); signErr == nil {
				view.MockupPreviewURL = u
				view.MockupPreviewURLExpiresAt = expires
			}
		}
	}
	return nil
}

func validateTextsFromPublished(tpl ports.PublishedTemplate, input map[string]string) (map[string]string, error) {
	editable := make([]domain.TextFieldSpec, 0, len(tpl.TextFields.Editable))
	for _, e := range tpl.TextFields.Editable {
		editable = append(editable, domain.TextFieldSpec{
			Key: e.Key, MaxLength: e.MaxLength, AllowedCharset: e.AllowedCharset,
		})
	}
	fixed := make(map[string]string, len(tpl.TextFields.Fixed))
	for _, f := range tpl.TextFields.Fixed {
		fixed[f.Key] = f.Value
	}
	out, err := domain.ValidateDesignTexts(editable, fixed, input)
	if err != nil {
		var te *domain.ErrInvalidTexts
		if errors.As(err, &te) {
			return nil, &DesignError{Code: DesignErrInvalidTexts, Message: te.Message}
		}
		return nil, err
	}
	return out, nil
}

func mapTransition(err error) error {
	var it *domain.ErrInvalidTransition
	if errors.As(err, &it) {
		return &DesignError{Code: DesignErrInvalidTransition, Message: it.Error()}
	}
	return err
}
