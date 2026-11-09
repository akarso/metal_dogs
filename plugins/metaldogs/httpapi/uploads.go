package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/generation"
)

// UploadResponse is the 201 body for POST /api/customizer/uploads.
type UploadResponse struct {
	UploadID   string    `json:"upload_id"`
	PreviewURL string    `json:"preview_url"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	MimeType   string    `json:"mime_type"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// multipart envelope overhead above MaxPhotoBytes (boundaries, headers).
const uploadMultipartSlack = 1 << 20 // 1 MiB

// CreateUpload handles POST /api/customizer/uploads (multipart field "file").
// svc may be nil when Init could not wire DB/AssetStore → 503.
func CreateUpload(svc *application.UploadService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if svc == nil {
			writeJSONError(w, http.StatusServiceUnavailable, application.UploadErrUnavailable)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, int64(generation.MaxPhotoBytes)+uploadMultipartSlack)
		if err := r.ParseMultipartForm(int64(generation.MaxPhotoBytes) + uploadMultipartSlack); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) || isMultipartTooLarge(err) {
				writeJSONError(w, http.StatusRequestEntityTooLarge, application.UploadErrPayloadTooLarge)
				return
			}
			writeJSONError(w, http.StatusBadRequest, "bad_request")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request")
			return
		}
		defer file.Close()

		raw, err := io.ReadAll(io.LimitReader(file, int64(generation.MaxPhotoBytes)+1))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request")
			return
		}
		if len(raw) > generation.MaxPhotoBytes {
			writeJSONError(w, http.StatusRequestEntityTooLarge, application.UploadErrPayloadTooLarge)
			return
		}

		hint := ""
		if header != nil {
			hint = header.Filename
		}
		res, err := svc.Create(r.Context(), raw, hint)
		if err != nil {
			var ue *application.UploadError
			if errors.As(err, &ue) {
				writeJSONError(w, uploadStatus(ue.Code), ue.Code)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(UploadResponse{
			UploadID:   res.UploadID,
			PreviewURL: res.PreviewURL,
			Width:      res.Width,
			Height:     res.Height,
			MimeType:   res.MimeType,
			ExpiresAt:  res.ExpiresAt.UTC(),
		})
	})
}

func uploadStatus(code string) int {
	switch code {
	case application.UploadErrPayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case application.UploadErrUnsupportedMedia:
		return http.StatusUnsupportedMediaType
	case application.UploadErrInvalidImage, application.UploadErrDimensionsOutOfRange:
		return http.StatusUnprocessableEntity
	case application.UploadErrUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func isMultipartTooLarge(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "request body too large")
}
