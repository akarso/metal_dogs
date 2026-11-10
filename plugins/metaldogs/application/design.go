package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// DesignError is a classified design failure for HTTP mapping.
type DesignError struct {
	Code    string
	Message string
}

func (e *DesignError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

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
)

// DesignView is the customer-facing design projection (no token hash).
type DesignView struct {
	ID                        string
	Status                    string
	TemplateID                string
	TemplateVersion           int
	Texts                     map[string]string
	UploadID                  string
	CreatedAt                 time.Time
	ExpiresAt                 time.Time
	SourcePreviewURL          string
	SourcePreviewURLExpiresAt time.Time
	OwnerToken                string // set only on Create
}

// DesignService coordinates design create/get/patch with ownership tokens.
type DesignService struct {
	Designs   ports.DesignRepository
	Assets    ports.AssetRepository
	Templates *TemplateCatalog
	Store     ports.AssetStore
	IDs       ports.IDGenerator
	Clock     ports.Clock
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

// Create builds a draft design, binds the upload, and returns the owner token once.
func (s *DesignService) Create(ctx context.Context, in CreateInput) (DesignView, error) {
	if s == nil || s.Designs == nil || s.Assets == nil || s.Templates == nil || s.Store == nil || s.IDs == nil || s.Clock == nil {
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
	if err := s.Designs.CreateBound(ctx, d); err != nil {
		if errors.Is(err, ports.ErrAssetBindConflict) {
			return DesignView{}, &DesignError{Code: DesignErrUploadAlreadyAttached, Message: "upload already attached"}
		}
		return DesignView{}, fmt.Errorf("design: create: %w", err)
	}
	view, err := s.toView(ctx, d, now)
	if err != nil {
		return DesignView{}, err
	}
	view.OwnerToken = token
	return view, nil
}

// Get returns a design when the bearer token matches.
func (s *DesignService) Get(ctx context.Context, id, bearerToken string) (DesignView, error) {
	d, err := s.loadAuthorized(ctx, id, bearerToken)
	if err != nil {
		return DesignView{}, err
	}
	return s.toView(ctx, d, s.Clock.Now().UTC())
}

// Patch updates texts and/or photo when authorized and editable.
func (s *DesignService) Patch(ctx context.Context, id, bearerToken string, in PatchInput) (DesignView, error) {
	d, err := s.loadAuthorized(ctx, id, bearerToken)
	if err != nil {
		return DesignView{}, err
	}
	now := s.Clock.Now().UTC()
	oldAsset := d.SourceAssetID
	changedPhoto := false

	if in.Texts != nil {
		tpl, ok, err := s.Templates.GetPublishedByID(ctx, d.TemplateID)
		if err != nil {
			return DesignView{}, fmt.Errorf("design: template: %w", err)
		}
		if !ok {
			// Pinned template may have been retired; still validate against last known fields if missing.
			return DesignView{}, &DesignError{Code: DesignErrTemplateUnavailable, Message: "template not published"}
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
	// If only texts changed (or same photo), AssertEditable already applied.
	if in.Texts == nil && in.UploadID != nil && !changedPhoto {
		if err := d.AssertEditable("ReplacePhoto"); err != nil {
			return DesignView{}, mapTransition(err)
		}
		d.UpdatedAt = now
	}

	if changedPhoto {
		if err := s.Designs.ReplaceSource(ctx, d, oldAsset); err != nil {
			if errors.Is(err, ports.ErrAssetBindConflict) {
				return DesignView{}, &DesignError{Code: DesignErrUploadAlreadyAttached, Message: "upload already attached"}
			}
			return DesignView{}, fmt.Errorf("design: replace source: %w", err)
		}
	} else {
		if err := s.Designs.UpdateEditable(ctx, d); err != nil {
			return DesignView{}, fmt.Errorf("design: update: %w", err)
		}
	}
	return s.toView(ctx, d, now)
}

func (s *DesignService) loadAuthorized(ctx context.Context, id, bearerToken string) (domain.Design, error) {
	if s == nil || s.Designs == nil || s.Clock == nil {
		return domain.Design{}, &DesignError{Code: DesignErrUnavailable, Message: "design deps missing"}
	}
	id = strings.TrimSpace(id)
	bearerToken = strings.TrimSpace(bearerToken)
	if id == "" || bearerToken == "" {
		return domain.Design{}, &DesignError{Code: DesignErrNotFound, Message: "not found"}
	}
	d, ok, err := s.Designs.GetByID(ctx, id)
	if err != nil {
		return domain.Design{}, fmt.Errorf("design: get: %w", err)
	}
	if !ok || !OwnerTokenMatches(bearerToken, d.OwnerTokenHash) {
		return domain.Design{}, &DesignError{Code: DesignErrNotFound, Message: "not found"}
	}
	return d, nil
}

func (s *DesignService) toView(ctx context.Context, d domain.Design, now time.Time) (DesignView, error) {
	asset, ok, err := s.Assets.GetByID(ctx, d.SourceAssetID)
	if err != nil {
		return DesignView{}, fmt.Errorf("design: source asset: %w", err)
	}
	preview := ""
	previewExp := time.Time{}
	if ok {
		url, err := s.Store.SignedGetURL(ctx, asset.StorageKey, ports.DefaultSignedGetTTL)
		if err != nil {
			return DesignView{}, fmt.Errorf("design: signed url: %w", err)
		}
		preview = url
		previewExp = now.Add(ports.DefaultSignedGetTTL)
	}
	return DesignView{
		ID:                        d.ID,
		Status:                    d.Status,
		TemplateID:                d.TemplateID,
		TemplateVersion:           d.TemplateVersion,
		Texts:                     d.Texts,
		UploadID:                  d.SourceAssetID,
		CreatedAt:                 d.CreatedAt,
		ExpiresAt:                 d.ExpiresAt,
		SourcePreviewURL:          preview,
		SourcePreviewURLExpiresAt: previewExp,
	}, nil
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
