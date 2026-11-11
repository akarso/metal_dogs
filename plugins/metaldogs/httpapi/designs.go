package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
)

type designErrorBody struct {
	Error     string `json:"error"`
	VersionID string `json:"version_id,omitempty"`
	DesignID  string `json:"design_id,omitempty"`
}

// DesignResponse is the customer-facing design DTO.
type DesignResponse struct {
	ID                          string            `json:"id"`
	Status                      string            `json:"status"`
	TemplateID                  string            `json:"template_id"`
	TemplateVersion             int               `json:"template_version"`
	Texts                       map[string]string `json:"texts"`
	UploadID                    string            `json:"upload_id,omitempty"`
	CreatedAt                   time.Time         `json:"created_at"`
	ExpiresAt                   time.Time         `json:"expires_at"`
	SourcePreviewURL            string            `json:"source_preview_url"`
	SourcePreviewURLExpiresAt   *time.Time        `json:"source_preview_url_expires_at,omitempty"`
	ArtworkPreviewURL           string            `json:"artwork_preview_url,omitempty"`
	ArtworkPreviewURLExpiresAt  *time.Time        `json:"artwork_preview_url_expires_at,omitempty"`
	MockupPreviewURL            string            `json:"mockup_preview_url,omitempty"`
	MockupPreviewURLExpiresAt   *time.Time        `json:"mockup_preview_url_expires_at,omitempty"`
	ApprovedVersionID           string            `json:"approved_version_id,omitempty"`
	OwnerToken                  string            `json:"owner_token,omitempty"`
	GenerationAttempts          int               `json:"generation_attempts"`
	GenerationAttemptsRemaining int               `json:"generation_attempts_remaining"`
	MaxGenerationAttempts       int               `json:"max_generation_attempts"`
}

// VersionResponse is the frozen DesignVersion DTO (C6).
type VersionResponse struct {
	ID               string            `json:"id"`
	DesignID         string            `json:"design_id"`
	TemplateID       string            `json:"template_id"`
	TemplateVersion  int               `json:"template_version"`
	Texts            map[string]string `json:"texts"`
	ArtworkAssetID   string            `json:"artwork_asset_id"`
	ArtworkChecksum  string            `json:"artwork_checksum"`
	ProductionStatus string            `json:"production_status"`
	Provider         string            `json:"provider"`
	Model            string            `json:"model"`
	PromptVersion    string            `json:"prompt_version"`
	ApprovedAt       time.Time         `json:"approved_at"`
}

// ApproveResponse is POST .../approve.
type ApproveResponse struct {
	Design  DesignResponse  `json:"design"`
	Version VersionResponse `json:"version"`
}

type createDesignBody struct {
	TemplateID string            `json:"template_id"`
	UploadID   string            `json:"upload_id"`
	Texts      map[string]string `json:"texts"`
}

type patchDesignBody struct {
	Texts    *map[string]string `json:"texts"`
	UploadID *string            `json:"upload_id"`
}

// CreateDesign handles POST /api/customizer/designs.
func CreateDesign(svc *application.DesignService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, application.DesignErrUnavailable)
			return
		}
		if tokenInQuery(r) {
			writeJSONError(w, http.StatusBadRequest, application.DesignErrBadRequest)
			return
		}
		var body createDesignBody
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, application.DesignErrBadRequest)
			return
		}
		if !looksLikeUUID(strings.TrimSpace(body.TemplateID)) || !looksLikeUUID(strings.TrimSpace(body.UploadID)) {
			writeJSONError(w, http.StatusBadRequest, application.DesignErrBadRequest)
			return
		}
		view, err := svc.Create(r.Context(), application.CreateInput{
			TemplateID: body.TemplateID,
			UploadID:   body.UploadID,
			Texts:      body.Texts,
		})
		if err != nil {
			writeDesignError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(toDesignResponse(view))
	})
}

// GetDesign handles GET /api/customizer/designs/{id}.
func GetDesign(svc *application.DesignService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, application.DesignErrUnavailable)
			return
		}
		if tokenInQuery(r) {
			writeJSONError(w, http.StatusBadRequest, application.DesignErrBadRequest)
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" || !looksLikeUUID(id) {
			writeJSONError(w, http.StatusNotFound, application.DesignErrNotFound)
			return
		}
		token, ok := bearerToken(r)
		if !ok {
			writeJSONError(w, http.StatusNotFound, application.DesignErrNotFound)
			return
		}
		view, err := svc.Get(r.Context(), id, token)
		if err != nil {
			writeDesignError(w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(toDesignResponse(view))
	})
}

// PatchDesign handles PATCH /api/customizer/designs/{id}.
func PatchDesign(svc *application.DesignService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, application.DesignErrUnavailable)
			return
		}
		if tokenInQuery(r) {
			writeJSONError(w, http.StatusBadRequest, application.DesignErrBadRequest)
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" || !looksLikeUUID(id) {
			writeJSONError(w, http.StatusNotFound, application.DesignErrNotFound)
			return
		}
		token, ok := bearerToken(r)
		if !ok {
			writeJSONError(w, http.StatusNotFound, application.DesignErrNotFound)
			return
		}
		var body patchDesignBody
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, application.DesignErrBadRequest)
			return
		}
		if body.UploadID != nil && !looksLikeUUID(strings.TrimSpace(*body.UploadID)) {
			writeJSONError(w, http.StatusBadRequest, application.DesignErrBadRequest)
			return
		}
		in := application.PatchInput{UploadID: body.UploadID}
		if body.Texts != nil {
			in.Texts = *body.Texts
			if in.Texts == nil {
				in.Texts = map[string]string{}
			}
		}
		view, err := svc.Patch(r.Context(), id, token, in)
		if err != nil {
			writeDesignError(w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(toDesignResponse(view))
	})
}

// ApproveDesign handles POST /api/customizer/designs/{id}/approve.
func ApproveDesign(svc *application.DesignService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, application.DesignErrUnavailable)
			return
		}
		if tokenInQuery(r) {
			writeJSONError(w, http.StatusBadRequest, application.DesignErrBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // empty body expected; bound anyway
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" || !looksLikeUUID(id) {
			writeJSONError(w, http.StatusNotFound, application.DesignErrNotFound)
			return
		}
		token, ok := bearerToken(r)
		if !ok {
			writeJSONError(w, http.StatusNotFound, application.DesignErrNotFound)
			return
		}
		res, err := svc.Approve(r.Context(), id, token)
		if err != nil {
			writeDesignError(w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ApproveResponse{
			Design:  toDesignResponse(res.Design),
			Version: toVersionResponse(res.Version),
		})
	})
}

// CloneDesign handles POST /api/customizer/designs/{id}/clone.
func CloneDesign(svc *application.DesignService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, application.DesignErrUnavailable)
			return
		}
		if tokenInQuery(r) {
			writeJSONError(w, http.StatusBadRequest, application.DesignErrBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" || !looksLikeUUID(id) {
			writeJSONError(w, http.StatusNotFound, application.DesignErrNotFound)
			return
		}
		token, ok := bearerToken(r)
		if !ok {
			writeJSONError(w, http.StatusNotFound, application.DesignErrNotFound)
			return
		}
		view, err := svc.Clone(r.Context(), id, token)
		if err != nil {
			writeDesignError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(toDesignResponse(view))
	})
}

func toDesignResponse(v application.DesignView) DesignResponse {
	texts := v.Texts
	if texts == nil {
		texts = map[string]string{}
	}
	resp := DesignResponse{
		ID:                          v.ID,
		Status:                      v.Status,
		TemplateID:                  v.TemplateID,
		TemplateVersion:             v.TemplateVersion,
		Texts:                       texts,
		UploadID:                    v.UploadID,
		CreatedAt:                   v.CreatedAt.UTC(),
		ExpiresAt:                   v.ExpiresAt.UTC(),
		SourcePreviewURL:            v.SourcePreviewURL,
		ArtworkPreviewURL:           v.ArtworkPreviewURL,
		MockupPreviewURL:            v.MockupPreviewURL,
		ApprovedVersionID:           v.ApprovedVersionID,
		OwnerToken:                  v.OwnerToken,
		GenerationAttempts:          v.GenerationAttempts,
		GenerationAttemptsRemaining: v.GenerationAttemptsRemaining,
		MaxGenerationAttempts:       v.MaxGenerationAttempts,
	}
	if !v.SourcePreviewURLExpiresAt.IsZero() {
		t := v.SourcePreviewURLExpiresAt.UTC()
		resp.SourcePreviewURLExpiresAt = &t
	}
	if !v.ArtworkPreviewURLExpiresAt.IsZero() {
		t := v.ArtworkPreviewURLExpiresAt.UTC()
		resp.ArtworkPreviewURLExpiresAt = &t
	}
	if !v.MockupPreviewURLExpiresAt.IsZero() {
		t := v.MockupPreviewURLExpiresAt.UTC()
		resp.MockupPreviewURLExpiresAt = &t
	}
	return resp
}

func toVersionResponse(v application.VersionView) VersionResponse {
	texts := v.Texts
	if texts == nil {
		texts = map[string]string{}
	}
	return VersionResponse{
		ID: v.ID, DesignID: v.DesignID, TemplateID: v.TemplateID, TemplateVersion: v.TemplateVersion,
		Texts: texts, ArtworkAssetID: v.ArtworkAssetID, ArtworkChecksum: v.ArtworkChecksum,
		ProductionStatus: v.ProductionStatus, Provider: v.Provider, Model: v.Model,
		PromptVersion: v.PromptVersion, ApprovedAt: v.ApprovedAt.UTC(),
	}
}

func writeDesignError(w http.ResponseWriter, err error) {
	var de *application.DesignError
	if errors.As(err, &de) {
		status := designStatus(de.Code)
		if de.Code == application.DesignErrUnavailable && de.VersionID != "" {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(designErrorBody{
				Error: de.Code, VersionID: de.VersionID, DesignID: de.DesignID,
			})
			return
		}
		if de.Code == application.DesignErrTooManyRequests {
			w.Header().Set("Retry-After", "5")
		}
		writeJSONError(w, status, de.Code)
		return
	}
	log.Printf("metaldogs.design.internal_error: %v", err)
	writeJSONError(w, http.StatusInternalServerError, "internal_error")
}

func designStatus(code string) int {
	switch code {
	case application.DesignErrBadRequest:
		return http.StatusBadRequest
	case application.DesignErrNotFound:
		return http.StatusNotFound
	case application.DesignErrTemplateUnavailable, application.DesignErrUploadUnavailable,
		application.DesignErrUploadAlreadyAttached, application.DesignErrInvalidTexts,
		application.DesignErrChecksumMismatch, application.DesignErrPayloadTooLarge:
		return http.StatusUnprocessableEntity
	case application.DesignErrInvalidTransition, application.DesignErrConflict:
		return http.StatusConflict
	case application.DesignErrTooManyRequests:
		return http.StatusTooManyRequests
	case application.DesignErrUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func bearerToken(r *http.Request) (string, bool) {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if h == "" {
		return "", false
	}
	const prefix = "Bearer "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}

func tokenInQuery(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("owner_token") != "" || q.Get("token") != "" || q.Get("access_token") != ""
}
