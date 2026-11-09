package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// PublicTemplate is the customer-facing template DTO (C1).
// Never includes prompts, model parameters, hashes, or reference paths.
type PublicTemplate struct {
	ID                string                   `json:"id"`
	Slug              string                   `json:"slug"`
	Genre             string                   `json:"genre"`
	StyleCategory     string                   `json:"style_category"`
	Version           int                      `json:"version"`
	PublishedAt       *time.Time               `json:"published_at"`
	PhotoRequirements domain.PhotoRequirements `json:"photo_requirements"`
	TextFields        domain.TextFields        `json:"text_fields"`
	OutputConfig      domain.OutputConfig      `json:"output_config"`
	ThumbnailURL      *string                  `json:"thumbnail_url"` // null until asset ingest
}

func toPublicTemplate(t domain.Template) PublicTemplate {
	return PublicTemplate{
		ID:                t.ID,
		Slug:              t.Slug,
		Genre:             t.Genre,
		StyleCategory:     t.StyleCategory,
		Version:           t.Version,
		PublishedAt:       t.PublishedAt,
		PhotoRequirements: t.PhotoRequirements,
		TextFields:        t.TextFields,
		OutputConfig:      t.OutputConfig,
		ThumbnailURL:      nil,
	}
}

func catalogFromApp(app *plugin.App) (*application.TemplateCatalog, bool) {
	if app == nil || app.Bootstrap == nil || app.Bootstrap.DB == nil {
		return nil, false
	}
	return &application.TemplateCatalog{Store: postgres.NewTemplateStore(app.Bootstrap.DB)}, true
}

// ListTemplates returns GET /api/customizer/templates?genre=.
// Resolves TemplateStore from app.Bootstrap.DB per request (fail closed → 503).
func ListTemplates(app *plugin.App) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		cat, ok := catalogFromApp(app)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		serveListTemplates(w, r, cat)
	})
}

// ListTemplatesWithCatalog is the testable core for list (injected store).
func ListTemplatesWithCatalog(cat *application.TemplateCatalog) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		serveListTemplates(w, r, cat)
	})
}

func serveListTemplates(w http.ResponseWriter, r *http.Request, cat *application.TemplateCatalog) {
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
}

// GetTemplate returns GET /api/customizer/templates/{id} (published UUID only).
func GetTemplate(app *plugin.App) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		cat, ok := catalogFromApp(app)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		serveGetTemplate(w, r, cat)
	})
}

// GetTemplateWithCatalog is the testable core for get (injected store).
func GetTemplateWithCatalog(cat *application.TemplateCatalog) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		serveGetTemplate(w, r, cat)
	})
}

func serveGetTemplate(w http.ResponseWriter, r *http.Request, cat *application.TemplateCatalog) {
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
}

func writeJSONError(w http.ResponseWriter, code int, errCode string) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": errCode})
}

func looksLikeUUID(s string) bool {
	// Accept canonical UUID shape only (C1 path is UUID id). Invalid → 404.
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
