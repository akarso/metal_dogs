package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
)

// DesignResponse is the customer-facing design DTO.
type DesignResponse struct {
	ID                        string            `json:"id"`
	Status                    string            `json:"status"`
	TemplateID                string            `json:"template_id"`
	TemplateVersion           int               `json:"template_version"`
	Texts                     map[string]string `json:"texts"`
	UploadID                  string            `json:"upload_id"`
	CreatedAt                 time.Time         `json:"created_at"`
	ExpiresAt                 time.Time         `json:"expires_at"`
	SourcePreviewURL          string            `json:"source_preview_url"`
	SourcePreviewURLExpiresAt *time.Time        `json:"source_preview_url_expires_at,omitempty"`
	OwnerToken                string            `json:"owner_token,omitempty"`
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

func toDesignResponse(v application.DesignView) DesignResponse {
	texts := v.Texts
	if texts == nil {
		texts = map[string]string{}
	}
	resp := DesignResponse{
		ID:               v.ID,
		Status:           v.Status,
		TemplateID:       v.TemplateID,
		TemplateVersion:  v.TemplateVersion,
		Texts:            texts,
		UploadID:         v.UploadID,
		CreatedAt:        v.CreatedAt.UTC(),
		ExpiresAt:        v.ExpiresAt.UTC(),
		SourcePreviewURL: v.SourcePreviewURL,
		OwnerToken:       v.OwnerToken,
	}
	if !v.SourcePreviewURLExpiresAt.IsZero() {
		t := v.SourcePreviewURLExpiresAt.UTC()
		resp.SourcePreviewURLExpiresAt = &t
	}
	return resp
}

func writeDesignError(w http.ResponseWriter, err error) {
	var de *application.DesignError
	if errors.As(err, &de) {
		writeJSONError(w, designStatus(de.Code), de.Code)
		return
	}
	writeJSONError(w, http.StatusInternalServerError, "internal_error")
}

func designStatus(code string) int {
	switch code {
	case application.DesignErrBadRequest:
		return http.StatusBadRequest
	case application.DesignErrNotFound:
		return http.StatusNotFound
	case application.DesignErrTemplateUnavailable, application.DesignErrUploadUnavailable,
		application.DesignErrUploadAlreadyAttached, application.DesignErrInvalidTexts:
		return http.StatusUnprocessableEntity
	case application.DesignErrInvalidTransition:
		return http.StatusConflict
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
