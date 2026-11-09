package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/generation"
)

// UploadResponse is the 201 body for POST /api/customizer/uploads.
type UploadResponse struct {
	UploadID            string    `json:"upload_id"`
	PreviewURL          string    `json:"preview_url"`
	PreviewURLExpiresAt time.Time `json:"preview_url_expires_at"`
	DeleteAfter         time.Time `json:"delete_after"`
	Width               int       `json:"width"`
	Height              int       `json:"height"`
	MimeType            string    `json:"mime_type"`
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
		raw, err := readMultipartFile(r, "file", int64(generation.MaxPhotoBytes))
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) || errors.Is(err, errPayloadTooLarge) {
				writeJSONError(w, http.StatusRequestEntityTooLarge, application.UploadErrPayloadTooLarge)
				return
			}
			if errors.Is(err, errBadRequest) {
				writeJSONError(w, http.StatusBadRequest, "bad_request")
				return
			}
			writeJSONError(w, http.StatusBadRequest, "bad_request")
			return
		}

		res, err := svc.Create(r.Context(), raw)
		if err != nil {
			var ue *application.UploadError
			if errors.As(err, &ue) {
				status := uploadStatus(ue.Code)
				if status == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "1")
				}
				writeJSONError(w, status, ue.Code)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(UploadResponse{
			UploadID:            res.UploadID,
			PreviewURL:          res.PreviewURL,
			PreviewURLExpiresAt: res.PreviewURLExpiresAt.UTC(),
			DeleteAfter:         res.DeleteAfter.UTC(),
			Width:               res.Width,
			Height:              res.Height,
			MimeType:            res.MimeType,
		})
	})
}

var (
	errBadRequest      = errors.New("bad_request")
	errPayloadTooLarge = errors.New("payload_too_large")
)

// readMultipartFile streams the named file part with a hard byte cap (no full-form memory copy).
func readMultipartFile(r *http.Request, field string, maxBytes int64) ([]byte, error) {
	ct := r.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "multipart/form-data" {
		return nil, errBadRequest
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, errBadRequest
	}
	mr := multipart.NewReader(r.Body, boundary)
	found := false
	var raw []byte
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				return nil, errPayloadTooLarge
			}
			return nil, errBadRequest
		}
		name := part.FormName()
		if name != field {
			_, _ = io.Copy(io.Discard, io.LimitReader(part, maxBytes+1))
			_ = part.Close()
			continue
		}
		found = true
		limited := io.LimitReader(part, maxBytes+1)
		raw, err = io.ReadAll(limited)
		_ = part.Close()
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				return nil, errPayloadTooLarge
			}
			return nil, errBadRequest
		}
		if int64(len(raw)) > maxBytes {
			return nil, errPayloadTooLarge
		}
		// Drain remaining parts so MaxBytesReader still applies to the whole body.
		for {
			extra, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				var maxErr *http.MaxBytesError
				if errors.As(err, &maxErr) {
					return nil, errPayloadTooLarge
				}
				break
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(extra, maxBytes+1))
			_ = extra.Close()
		}
		break
	}
	if !found {
		return nil, errBadRequest
	}
	return raw, nil
}

func uploadStatus(code string) int {
	switch code {
	case application.UploadErrPayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case application.UploadErrUnsupportedMedia:
		return http.StatusUnsupportedMediaType
	case application.UploadErrInvalidImage, application.UploadErrDimensionsOutOfRange:
		return http.StatusUnprocessableEntity
	case application.UploadErrTooManyRequests:
		return http.StatusTooManyRequests
	case application.UploadErrUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
