package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"github.com/akarso/shopanda/plugins/metaldogs/generation/imagemime"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// MaxArtworkBytes caps artwork Read during approve (PNG canvas bound).
const MaxArtworkBytes = 50 << 20 // 50 MiB

// VersionView is the customer-facing DesignVersion projection.
type VersionView struct {
	ID               string
	DesignID         string
	TemplateID       string
	TemplateVersion  int
	Texts            map[string]string
	ArtworkAssetID   string
	ArtworkChecksum  string
	ProductionStatus string
	Provider         string
	Model            string
	PromptVersion    string
	ApprovedAt       time.Time
}

// ApproveResult is returned by Approve (design + frozen version).
type ApproveResult struct {
	Design  DesignView
	Version VersionView
}

// Approve freezes the current artwork as a DesignVersion and marks the design approved.
// Idempotent when already approved: returns the existing version and re-attempts prepare enqueue.
// If the version is committed but enqueue fails → 503 unavailable with VersionID set.
func (s *DesignService) Approve(ctx context.Context, designID, bearerToken string) (ApproveResult, error) {
	if !s.depsReady() || s.Versions == nil || s.Jobs == nil {
		return ApproveResult{}, &DesignError{Code: DesignErrUnavailable, Message: "approve deps missing"}
	}
	d, now, err := s.loadAuthorized(ctx, designID, bearerToken)
	if err != nil {
		return ApproveResult{}, err
	}
	if d.Status == domain.DesignApproved {
		return s.approveIdempotent(ctx, d, now)
	}
	if d.Status != domain.DesignPreviewReady {
		return ApproveResult{}, &DesignError{Code: DesignErrInvalidTransition, Message: "approve requires preview_ready"}
	}
	if d.LatestJobID == "" {
		return ApproveResult{}, &DesignError{Code: DesignErrInvalidTransition, Message: "no succeeded generation"}
	}
	job, ok, err := s.Jobs.GetByID(ctx, d.LatestJobID)
	if err != nil {
		return ApproveResult{}, fmt.Errorf("approve: job: %w", err)
	}
	if !ok || job.Status != domain.JobSucceeded || job.DesignID != d.ID || job.ArtworkAssetID == "" {
		return ApproveResult{}, &DesignError{Code: DesignErrInvalidTransition, Message: "latest job not succeeded with artwork"}
	}
	if strings.TrimSpace(job.Provider) == "" || strings.TrimSpace(job.Model) == "" || strings.TrimSpace(job.PromptVersion) == "" {
		return ApproveResult{}, &DesignError{Code: DesignErrInvalidTransition, Message: "generation metadata incomplete"}
	}
	art, aok, err := s.Assets.GetByID(ctx, job.ArtworkAssetID)
	if err != nil {
		return ApproveResult{}, fmt.Errorf("approve: artwork: %w", err)
	}
	if !aok || art.Type != domain.AssetTypeArtwork || art.OwnerRef != d.ID || art.Checksum == "" || art.StorageKey == "" {
		return ApproveResult{}, &DesignError{Code: DesignErrInvalidTransition, Message: "artwork asset unavailable"}
	}
	if art.SizeBytes > MaxArtworkBytes {
		return ApproveResult{}, &DesignError{Code: DesignErrPayloadTooLarge, Message: "artwork too large"}
	}
	raw, err := s.Store.Read(ctx, art.StorageKey)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ApproveResult{}, &DesignError{Code: DesignErrChecksumMismatch, Message: "artwork blob missing"}
		}
		return ApproveResult{}, &DesignError{Code: DesignErrUnavailable, Message: "artwork blob unavailable"}
	}
	if int64(len(raw)) > MaxArtworkBytes {
		return ApproveResult{}, &DesignError{Code: DesignErrPayloadTooLarge, Message: "artwork too large"}
	}
	if art.SizeBytes > 0 && int64(len(raw)) != art.SizeBytes {
		return ApproveResult{}, &DesignError{Code: DesignErrChecksumMismatch, Message: "artwork size mismatch"}
	}
	sum := sha256.Sum256(raw)
	digest := ChecksumSHA256Prefix + hex.EncodeToString(sum[:])
	if digest != art.Checksum {
		return ApproveResult{}, &DesignError{Code: DesignErrChecksumMismatch, Message: "artwork checksum mismatch"}
	}

	versionID := s.IDs.New()
	v, err := domain.NewDesignVersion(
		versionID, d.ID, d.TemplateID, d.TemplateVersion, d.Texts,
		art.ID, digest,
		job.Provider, job.Model, job.PromptVersion, job.ParametersJSON, now,
	)
	if err != nil {
		return ApproveResult{}, fmt.Errorf("approve: version: %w", err)
	}
	if err := d.Approve(versionID, now); err != nil {
		return ApproveResult{}, mapTransition(err)
	}
	if err := s.Versions.ApproveBound(ctx, d, v); err != nil {
		if errors.Is(err, ports.ErrDesignConflict) {
			cur, ok, gerr := s.Designs.GetByID(ctx, d.ID)
			if gerr != nil {
				return ApproveResult{}, fmt.Errorf("approve: re-read: %w", gerr)
			}
			if ok && cur.Status == domain.DesignApproved {
				return s.approveIdempotent(ctx, cur, now)
			}
			// Left preview_ready without approving (patch/generate won, or orphan version race).
			return ApproveResult{}, &DesignError{Code: DesignErrInvalidTransition, Message: "approve race; design not approved"}
		}
		return ApproveResult{}, fmt.Errorf("approve: bind: %w", err)
	}
	if err := s.ensurePrepareEnqueued(ctx, v); err != nil {
		s.logPrepareEnqueue(d.ID, v.ID, err)
		return ApproveResult{}, &DesignError{
			Code: DesignErrUnavailable, Message: "prepare enqueue failed",
			VersionID: v.ID, DesignID: d.ID,
		}
	}
	view, err := s.approveViewAfterCommit(ctx, d, now)
	if err != nil {
		return ApproveResult{Version: toVersionView(v)}, s.approveViewError(d.ID, v.ID, err)
	}
	return ApproveResult{Design: view, Version: toVersionView(v)}, nil
}

func (s *DesignService) approveIdempotent(ctx context.Context, d domain.Design, now time.Time) (ApproveResult, error) {
	v, err := s.loadApprovedVersion(ctx, d)
	if err != nil {
		return ApproveResult{}, err
	}
	if err := s.ensurePrepareEnqueued(ctx, v); err != nil {
		s.logPrepareEnqueue(d.ID, v.ID, err)
		return ApproveResult{}, &DesignError{
			Code: DesignErrUnavailable, Message: "prepare enqueue failed",
			VersionID: v.ID, DesignID: d.ID,
		}
	}
	view, err := s.approveViewAfterCommit(ctx, d, now)
	if err != nil {
		return ApproveResult{Version: toVersionView(v)}, s.approveViewError(d.ID, v.ID, err)
	}
	return ApproveResult{Design: view, Version: toVersionView(v)}, nil
}

func (s *DesignService) approveViewError(designID, versionID string, cause error) error {
	var de *DesignError
	if errors.As(cause, &de) {
		out := *de
		out.VersionID = versionID
		if out.DesignID == "" {
			out.DesignID = designID
		}
		return &out
	}
	// Source-asset I/O and other plain wraps stay 500 — same as GET (do not promote to 503).
	return cause
}

func (s *DesignService) logPrepareEnqueue(designID, versionID string, err error) {
	if s.OnPrepareEnqueueError != nil {
		s.OnPrepareEnqueueError(designID, versionID, err)
	}
}

func (s *DesignService) loadApprovedVersion(ctx context.Context, d domain.Design) (domain.DesignVersion, error) {
	if d.ApprovedVersionID != "" {
		v, ok, err := s.Versions.GetByID(ctx, d.ApprovedVersionID)
		if err != nil {
			return domain.DesignVersion{}, fmt.Errorf("approve: load version by id: %w", err)
		}
		if !ok {
			return domain.DesignVersion{}, &DesignError{Code: DesignErrUnavailable, Message: "approved version missing"}
		}
		if v.DesignID != d.ID {
			return domain.DesignVersion{}, &DesignError{Code: DesignErrUnavailable, Message: "approved version design mismatch"}
		}
		return v, nil
	}
	v, ok, err := s.Versions.GetByDesignID(ctx, d.ID)
	if err != nil {
		return domain.DesignVersion{}, fmt.Errorf("approve: load version: %w", err)
	}
	if !ok {
		return domain.DesignVersion{}, &DesignError{Code: DesignErrUnavailable, Message: "approved version missing"}
	}
	return v, nil
}

// ensurePrepareEnqueued runs after the version exists. Skips when production is
// already ready (G5 finished). Nil EnqueuePrepare skips (unit fakes).
func (s *DesignService) ensurePrepareEnqueued(parent context.Context, v domain.DesignVersion) error {
	if s.EnqueuePrepare == nil {
		return nil
	}
	if v.ProductionStatus == domain.ProductionReady {
		return nil
	}
	if parent == nil {
		parent = context.Background()
	}
	// Approve waits for enqueue so a failure can return 503; WithoutCancel keeps
	// the attempt alive if the client disconnects mid-call.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	return s.EnqueuePrepare(ctx, v.ID, v.DesignID)
}

// Clone creates a new draft from an approved design, copying source bytes to a new asset.
func (s *DesignService) Clone(ctx context.Context, designID, bearerToken string) (DesignView, error) {
	if !s.depsReady() {
		return DesignView{}, &DesignError{Code: DesignErrUnavailable, Message: "design deps missing"}
	}
	if !s.acquireClone() {
		return DesignView{}, &DesignError{Code: DesignErrTooManyRequests, Message: "clone concurrency limit"}
	}
	defer s.releaseClone()

	src, now, err := s.loadAuthorized(ctx, designID, bearerToken)
	if err != nil {
		return DesignView{}, err
	}
	token, hash, err := MintOwnerToken()
	if err != nil {
		return DesignView{}, fmt.Errorf("clone: mint token: %w", err)
	}
	newDesignID := s.IDs.New()
	newUploadID := s.IDs.New()

	srcAsset, ok, err := s.Assets.GetByID(ctx, src.SourceAssetID)
	if err != nil {
		return DesignView{}, fmt.Errorf("clone: source asset: %w", err)
	}
	if !ok || srcAsset.StorageKey == "" || srcAsset.Type != domain.AssetTypeSource {
		return DesignView{}, &DesignError{Code: DesignErrUploadUnavailable, Message: "source unavailable"}
	}
	if srcAsset.OwnerRef != src.ID {
		return DesignView{}, &DesignError{Code: DesignErrUploadUnavailable, Message: "source owner mismatch"}
	}
	if srcAsset.SizeBytes <= 0 || srcAsset.SizeBytes > int64(generation.MaxPhotoBytes) {
		return DesignView{}, &DesignError{Code: DesignErrUploadUnavailable, Message: "source size invalid"}
	}
	raw, err := s.Store.Read(ctx, srcAsset.StorageKey)
	if err != nil {
		return DesignView{}, fmt.Errorf("clone: read source: %w", err)
	}
	if int64(len(raw)) != srcAsset.SizeBytes || len(raw) > generation.MaxPhotoBytes {
		return DesignView{}, &DesignError{Code: DesignErrChecksumMismatch, Message: "source size mismatch"}
	}
	sum := sha256.Sum256(raw)
	digest := ChecksumSHA256Prefix + hex.EncodeToString(sum[:])
	if digest != srcAsset.Checksum {
		return DesignView{}, &DesignError{Code: DesignErrChecksumMismatch, Message: "source checksum mismatch"}
	}
	mime := imagemime.Sniff(raw)
	var ext string
	switch mime {
	case imagemime.JPEG:
		ext = "jpg"
	case imagemime.PNG:
		ext = "png"
	default:
		return DesignView{}, &DesignError{Code: DesignErrUploadUnavailable, Message: "source media unsupported"}
	}
	key, err := ports.ObjectKey(domain.AssetTypeSource, newUploadID, ext, now)
	if err != nil {
		return DesignView{}, fmt.Errorf("clone: object key: %w", err)
	}
	if err := s.Store.Put(ctx, key, bytes.NewReader(raw), mime); err != nil {
		return DesignView{}, fmt.Errorf("clone: put source: %w", err)
	}
	newAsset := domain.Asset{
		ID: newUploadID, Type: domain.AssetTypeSource, StorageKey: key,
		MimeType: mime, SizeBytes: int64(len(raw)),
		Width: srcAsset.Width, Height: srcAsset.Height,
		Checksum:  digest,
		CreatedAt: now, DeleteAfter: now.Add(UploadRetention),
	}
	cleanup := func() {
		cctx := context.WithoutCancel(ctx)
		// Soft-delete first so a live row never points at a deleted blob (upload rule).
		if softErr := s.Assets.SoftDelete(cctx, newUploadID, now); softErr != nil {
			s.cleanupErr("soft_delete", newUploadID, key, softErr)
			return
		}
		if delErr := s.Store.Delete(cctx, key); delErr != nil && !errors.Is(delErr, os.ErrNotExist) {
			s.cleanupErr("delete", newUploadID, key, delErr)
			return
		}
		// Missing object is success for Delete; still clear the soft-deleted row's key.
		if clearErr := s.Assets.ClearStorageKey(cctx, newUploadID); clearErr != nil {
			s.cleanupErr("clear_storage_key", newUploadID, key, clearErr)
		}
	}
	if err := s.Assets.Insert(ctx, newAsset); err != nil {
		// Soft-delete first only applies once a row exists; blob-only cleanup here.
		cctx := context.WithoutCancel(ctx)
		if delErr := s.Store.Delete(cctx, key); delErr != nil {
			s.cleanupErr("delete", newUploadID, key, delErr)
		}
		return DesignView{}, fmt.Errorf("clone: insert asset: %w", err)
	}
	draft, err := src.CloneDraft(newDesignID, newUploadID, hash, now)
	if err != nil {
		cleanup()
		return DesignView{}, mapTransition(err)
	}
	deleteAfter := now.Add(domain.SourceAttachedRetention)
	if err := s.Designs.CreateBound(ctx, draft, deleteAfter); err != nil {
		cleanup()
		if errors.Is(err, ports.ErrAssetBindConflict) {
			return DesignView{}, &DesignError{Code: DesignErrUploadAlreadyAttached, Message: "upload already attached"}
		}
		return DesignView{}, fmt.Errorf("clone: create: %w", err)
	}
	view := s.createCloneView(ctx, draft, now)
	view.OwnerToken = token
	return view, nil
}

func toVersionView(v domain.DesignVersion) VersionView {
	texts := v.Texts
	if texts == nil {
		texts = map[string]string{}
	}
	return VersionView{
		ID: v.ID, DesignID: v.DesignID, TemplateID: v.TemplateID, TemplateVersion: v.TemplateVersion,
		Texts: texts, ArtworkAssetID: v.ArtworkAssetID, ArtworkChecksum: v.ArtworkChecksum,
		ProductionStatus: v.ProductionStatus, Provider: v.Provider, Model: v.Model,
		PromptVersion: v.PromptVersion, ApprovedAt: v.ApprovedAt,
	}
}
