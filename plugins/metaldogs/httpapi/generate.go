package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
)

// GenerateAccepted is the 202 response for POST .../generate.
type GenerateAccepted struct {
	JobID      string `json:"job_id"`
	DesignID   string `json:"design_id"`
	Status     string `json:"status"`
	RetryAfter int    `json:"retry_after"`
}

// GenerationJobResponse is GET /api/customizer/generation-jobs/{id}.
type GenerationJobResponse struct {
	ID             string `json:"id"`
	DesignID       string `json:"design_id"`
	Status         string `json:"status"`
	Attempt        int    `json:"attempt"`
	ErrorCode      string `json:"error_code,omitempty"`
	TerminalReason string `json:"terminal_reason,omitempty"`
	RetryAfter     int    `json:"retry_after"`
}

type generateErrorBody struct {
	Error      string `json:"error"`
	JobID      string `json:"job_id,omitempty"`
	DesignID   string `json:"design_id,omitempty"`
	RetryAfter int    `json:"retry_after,omitempty"`
}

// GenerateDesign handles POST /api/customizer/designs/{id}/generate.
func GenerateDesign(svc *application.GenerateService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, application.GenerateErrUnavailable)
			return
		}
		if tokenInQuery(r) {
			writeJSONError(w, http.StatusBadRequest, application.GenerateErrBadRequest)
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" || !looksLikeUUID(id) {
			writeJSONError(w, http.StatusNotFound, application.GenerateErrNotFound)
			return
		}
		token, ok := bearerToken(r)
		if !ok {
			writeJSONError(w, http.StatusNotFound, application.GenerateErrNotFound)
			return
		}
		idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		corr := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		view, err := svc.RequestGenerate(r.Context(), id, token, idem, corr)
		if err != nil {
			writeGenerateError(w, err)
			return
		}
		if view.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(view.RetryAfter))
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(GenerateAccepted{
			JobID:      view.ID,
			DesignID:   view.DesignID,
			Status:     view.Status,
			RetryAfter: view.RetryAfter,
		})
	})
}

// GetGenerationJob handles GET /api/customizer/generation-jobs/{id}.
func GetGenerationJob(svc *application.GenerateService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, application.GenerateErrUnavailable)
			return
		}
		if tokenInQuery(r) {
			writeJSONError(w, http.StatusBadRequest, application.GenerateErrBadRequest)
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" || !looksLikeUUID(id) {
			writeJSONError(w, http.StatusNotFound, application.GenerateErrNotFound)
			return
		}
		token, ok := bearerToken(r)
		if !ok {
			writeJSONError(w, http.StatusNotFound, application.GenerateErrNotFound)
			return
		}
		view, err := svc.GetJob(r.Context(), id, token)
		if err != nil {
			writeGenerateError(w, err)
			return
		}
		if view.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(view.RetryAfter))
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(GenerationJobResponse{
			ID:             view.ID,
			DesignID:       view.DesignID,
			Status:         view.Status,
			Attempt:        view.Attempt,
			ErrorCode:      view.ErrorCode,
			TerminalReason: view.TerminalReason,
			RetryAfter:     view.RetryAfter,
		})
	})
}

func writeGenerateError(w http.ResponseWriter, err error) {
	var ge *application.GenerateError
	if errors.As(err, &ge) {
		if ge.Code == application.GenerateErrUnavailable && ge.JobID != "" {
			log.Printf("metaldogs.generate.error code=%s design_id=%s job_id=%s: %v",
				ge.Code, ge.DesignID, ge.JobID, err)
		}
		status := generateStatus(ge.Code)
		if ge.Code == application.GenerateErrCooldown {
			retryAfter := ge.RetryAfter
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(generateErrorBody{
				Error: ge.Code, DesignID: ge.DesignID, JobID: ge.JobID, RetryAfter: retryAfter,
			})
			return
		}
		if ge.Code == application.GenerateErrLimitReached {
			// Permanent for this design — do not advertise Retry-After.
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(generateErrorBody{Error: ge.Code, DesignID: ge.DesignID})
			return
		}
		if ge.JobID != "" && (ge.Code == application.GenerateErrConflict ||
			ge.Code == application.GenerateErrInvalidTransition ||
			ge.Code == application.GenerateErrUnavailable) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(generateErrorBody{Error: ge.Code, JobID: ge.JobID, DesignID: ge.DesignID})
			return
		}
		writeJSONError(w, status, ge.Code)
		return
	}
	log.Printf("metaldogs.generate.internal_error: %v", err)
	writeJSONError(w, http.StatusInternalServerError, "internal_error")
}

func generateStatus(code string) int {
	switch code {
	case application.GenerateErrBadRequest:
		return http.StatusBadRequest
	case application.GenerateErrNotFound:
		return http.StatusNotFound
	case application.GenerateErrInvalidTransition, application.GenerateErrConflict:
		return http.StatusConflict
	case application.GenerateErrUploadUnavailable:
		return http.StatusUnprocessableEntity
	case application.GenerateErrLimitReached, application.GenerateErrCooldown:
		return http.StatusTooManyRequests
	case application.GenerateErrUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
