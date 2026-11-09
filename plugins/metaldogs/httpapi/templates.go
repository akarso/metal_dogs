package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// Transport DTOs — explicit copies of public fields (never embed domain types).

type PublicPhotoRequirements struct {
	Pose           string `json:"pose"`
	BackgroundHint string `json:"background_hint"`
	LightingHint   string `json:"lighting_hint"`
}

type PublicEditableText struct {
	Key            string `json:"key"`
	MaxLength      int    `json:"max_length"`
	AllowedCharset string `json:"allowed_charset"`
}

type PublicFixedText struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type PublicTextFields struct {
	Editable []PublicEditableText `json:"editable"`
	Fixed    []PublicFixedText    `json:"fixed"`
}

type PublicPrintArea struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type PublicOutputConfig struct {
	CanvasWidth  int             `json:"canvas_width"`
	CanvasHeight int             `json:"canvas_height"`
	Monochrome   bool            `json:"monochrome"`
	PrintArea    PublicPrintArea `json:"print_area"`
}

// PublicTemplate is the customer-facing template DTO (C1).
type PublicTemplate struct {
	ID                string                  `json:"id"`
	Slug              string                  `json:"slug"`
	Genre             string                  `json:"genre"`
	StyleCategory     string                  `json:"style_category"`
	Version           int                     `json:"version"`
	PublishedAt       *time.Time              `json:"published_at"`
	PhotoRequirements PublicPhotoRequirements `json:"photo_requirements"`
	TextFields        PublicTextFields        `json:"text_fields"`
	OutputConfig      PublicOutputConfig      `json:"output_config"`
	ThumbnailURL      *string                 `json:"thumbnail_url"` // null until asset ingest
}

func toPublicTemplate(t ports.PublishedTemplate) PublicTemplate {
	editable := make([]PublicEditableText, 0, len(t.TextFields.Editable))
	for _, e := range t.TextFields.Editable {
		editable = append(editable, PublicEditableText{
			Key: e.Key, MaxLength: e.MaxLength, AllowedCharset: e.AllowedCharset,
		})
	}
	fixed := make([]PublicFixedText, 0, len(t.TextFields.Fixed))
	for _, f := range t.TextFields.Fixed {
		fixed = append(fixed, PublicFixedText{Key: f.Key, Value: f.Value})
	}
	return PublicTemplate{
		ID:            t.ID,
		Slug:          t.Slug,
		Genre:         t.Genre,
		StyleCategory: t.StyleCategory,
		Version:       t.Version,
		PublishedAt:   t.PublishedAt,
		PhotoRequirements: PublicPhotoRequirements{
			Pose: t.PhotoRequirements.Pose, BackgroundHint: t.PhotoRequirements.BackgroundHint,
			LightingHint: t.PhotoRequirements.LightingHint,
		},
		TextFields: PublicTextFields{Editable: editable, Fixed: fixed},
		OutputConfig: PublicOutputConfig{
			CanvasWidth: t.OutputConfig.CanvasWidth, CanvasHeight: t.OutputConfig.CanvasHeight,
			Monochrome: t.OutputConfig.Monochrome,
			PrintArea: PublicPrintArea{
				X: t.OutputConfig.PrintArea.X, Y: t.OutputConfig.PrintArea.Y,
				Width: t.OutputConfig.PrintArea.Width, Height: t.OutputConfig.PrintArea.Height,
			},
		},
		ThumbnailURL: nil,
	}
}

// ListTemplates returns GET /api/customizer/templates?genre=.
// cat may be nil when Init could not wire a DB-backed catalog → 503.
func ListTemplates(cat *application.TemplateCatalog) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if cat == nil || cat.Store == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		genre := strings.TrimSpace(r.URL.Query().Get("genre"))
		rows, err := cat.ListPublished(r.Context(), genre)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		out := make([]PublicTemplate, 0, len(rows))
		for _, t := range rows {
			out = append(out, toPublicTemplate(t))
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"templates": out})
	})
}

// GetTemplate returns GET /api/customizer/templates/{id} (published UUID only).
func GetTemplate(cat *application.TemplateCatalog) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if cat == nil || cat.Store == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" || !looksLikeUUID(id) {
			writeJSONError(w, http.StatusNotFound, "not_found")
			return
		}
		t, ok, err := cat.GetPublishedByID(r.Context(), id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, "not_found")
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(toPublicTemplate(t))
	})
}

func writeJSONError(w http.ResponseWriter, code int, errCode string) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": errCode})
}

func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return false
			}
		}
	}
	return true
}
