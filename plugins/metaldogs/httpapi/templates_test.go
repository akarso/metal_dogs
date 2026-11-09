package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

type tplMem struct {
	rows map[string]domain.Template
	err  error
}

func (m *tplMem) InsertDraft(context.Context, domain.Template) error { return nil }
func (m *tplMem) LatestBySlug(context.Context, string) (domain.Template, bool, error) {
	return domain.Template{}, false, nil
}
func (m *tplMem) GetBySlugVersion(context.Context, string, int) (domain.Template, bool, error) {
	return domain.Template{}, false, nil
}
func (m *tplMem) LatestDraftBySlug(context.Context, string) (domain.Template, bool, error) {
	return domain.Template{}, false, nil
}
func (m *tplMem) DraftBySlugHash(context.Context, string, string) (domain.Template, bool, error) {
	return domain.Template{}, false, nil
}
func (m *tplMem) PublishedBySlug(context.Context, string) (domain.Template, bool, error) {
	return domain.Template{}, false, nil
}
func (m *tplMem) SetStatus(context.Context, string, string) error { return nil }
func (m *tplMem) PublishExclusive(context.Context, string, string) (domain.Template, error) {
	return domain.Template{}, nil
}
func (m *tplMem) RetireExclusive(context.Context, string) (domain.Template, error) {
	return domain.Template{}, nil
}

func publishedView(r domain.Template) ports.PublishedTemplate {
	return ports.PublishedTemplate{
		ID: r.ID, Slug: r.Slug, Genre: r.Genre, StyleCategory: r.StyleCategory,
		Version: r.Version, PublishedAt: r.PublishedAt,
		PhotoRequirements: r.PhotoRequirements, TextFields: r.TextFields, OutputConfig: r.OutputConfig,
	}
}

func (m *tplMem) ListPublished(_ context.Context, genre string) ([]ports.PublishedTemplate, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []ports.PublishedTemplate
	for _, r := range m.rows {
		if r.Status != domain.TemplatePublished {
			continue
		}
		if genre != "" && r.Genre != genre {
			continue
		}
		out = append(out, publishedView(r))
	}
	return out, nil
}

func (m *tplMem) GetPublishedByID(_ context.Context, id string) (ports.PublishedTemplate, bool, error) {
	if m.err != nil {
		return ports.PublishedTemplate{}, false, m.err
	}
	r, ok := m.rows[id]
	if !ok || r.Status != domain.TemplatePublished {
		return ports.PublishedTemplate{}, false, nil
	}
	return publishedView(r), true, nil
}

var _ ports.TemplateStore = (*tplMem)(nil)

func samplePublished(id, slug, genre string) domain.Template {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	return domain.Template{
		ID: id, Slug: slug, Status: domain.TemplatePublished, Genre: genre,
		StyleCategory: "head_portrait", Version: 1, ContentHash: "h",
		PromptInstructions: domain.PromptInstructions{Text: "SECRET PROMPT", PromptVersion: "g2"},
		ModelParameters:    domain.ModelParameters{Model: "secret-model", Width: 1024, Height: 1024},
		PhotoRequirements:  domain.PhotoRequirements{Pose: "head_front"},
		TextFields: domain.TextFields{
			Editable: []domain.EditableText{{Key: "dog_name", MaxLength: 12, AllowedCharset: "latin_polish"}},
			Fixed:    []domain.FixedText{},
		},
		OutputConfig: domain.OutputConfig{
			CanvasWidth: 1024, CanvasHeight: 1024, Monochrome: true,
			PrintArea: domain.PrintArea{Width: 1024, Height: 1024},
		},
		ReferencePatternPath: "secret/ref.png",
		ThumbnailPath:        "secret/thumb.png",
		PublishedAt:          &now,
	}
}

func assertNoSecrets(t *testing.T, raw string) {
	t.Helper()
	for _, leak := range []string{
		"SECRET PROMPT", "secret-model", "content_hash", "prompt_instructions",
		"model_parameters", "reference_pattern", "thumbnail_path", "secret/ref", "secret/thumb",
	} {
		if strings.Contains(raw, leak) {
			t.Fatalf("DTO leaked %q: %s", leak, raw)
		}
	}
}

func TestListTemplates_DTOExcludesSecretsAndNonPublished(t *testing.T) {
	t.Parallel()
	pubID := "11111111-1111-4111-8111-111111111111"
	draftID := "22222222-2222-4222-8222-222222222222"
	retiredID := "33333333-3333-4333-8333-333333333333"
	store := &tplMem{rows: map[string]domain.Template{
		pubID: samplePublished(pubID, "head-front-blackmetal", "black_metal"),
		draftID: func() domain.Template {
			d := samplePublished(draftID, "drafty", "punk")
			d.Status = domain.TemplateDraft
			return d
		}(),
		retiredID: func() domain.Template {
			r := samplePublished(retiredID, "retired-slug", "punk")
			r.Status = domain.TemplateRetired
			return r
		}(),
	}}
	cat := &application.TemplateCatalog{Store: store}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/customizer/templates", nil)
	httpapi.ListTemplates(cat).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	assertNoSecrets(t, raw)
	if strings.Contains(raw, "drafty") || strings.Contains(raw, "retired-slug") {
		t.Fatalf("non-published leaked: %s", raw)
	}
	var body struct {
		Templates []httpapi.PublicTemplate `json:"templates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Templates) != 1 || body.Templates[0].ID != pubID || body.Templates[0].ThumbnailURL != nil {
		t.Fatalf("body = %+v", body)
	}
}

func TestListTemplates_GenreFilter(t *testing.T) {
	t.Parallel()
	a := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	b := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	store := &tplMem{rows: map[string]domain.Template{
		a: samplePublished(a, "a", "black_metal"),
		b: samplePublished(b, "b", "punk"),
	}}
	cat := &application.TemplateCatalog{Store: store}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/customizer/templates?genre=punk", nil)
	httpapi.ListTemplates(cat).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Templates []httpapi.PublicTemplate `json:"templates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Templates) != 1 || body.Templates[0].Slug != "b" {
		t.Fatalf("body = %+v", body)
	}
}

func TestGetTemplate_HappyPathExcludesSecrets(t *testing.T) {
	t.Parallel()
	pubID := "11111111-1111-4111-8111-111111111111"
	store := &tplMem{rows: map[string]domain.Template{
		pubID: samplePublished(pubID, "pub", "punk"),
	}}
	cat := &application.TemplateCatalog{Store: store}
	mux := http.NewServeMux()
	mux.Handle("GET /api/customizer/templates/{id}", httpapi.GetTemplate(cat))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/templates/"+pubID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertNoSecrets(t, rec.Body.String())
	var dto httpapi.PublicTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatal(err)
	}
	if dto.ID != pubID || dto.Slug != "pub" || dto.ThumbnailURL != nil {
		t.Fatalf("dto = %+v", dto)
	}
}

func TestGetTemplate_404ForDraftRetiredUnknownAndBadID(t *testing.T) {
	t.Parallel()
	pubID := "11111111-1111-4111-8111-111111111111"
	draftID := "22222222-2222-4222-8222-222222222222"
	retiredID := "33333333-3333-4333-8333-333333333333"
	store := &tplMem{rows: map[string]domain.Template{
		pubID: samplePublished(pubID, "pub", "punk"),
		draftID: func() domain.Template {
			d := samplePublished(draftID, "draft", "punk")
			d.Status = domain.TemplateDraft
			return d
		}(),
		retiredID: func() domain.Template {
			r := samplePublished(retiredID, "retired", "punk")
			r.Status = domain.TemplateRetired
			return r
		}(),
	}}
	cat := &application.TemplateCatalog{Store: store}
	mux := http.NewServeMux()
	mux.Handle("GET /api/customizer/templates/{id}", httpapi.GetTemplate(cat))

	cases := []struct {
		path string
		code int
	}{
		{"/api/customizer/templates/" + pubID, http.StatusOK},
		{"/api/customizer/templates/" + draftID, http.StatusNotFound},
		{"/api/customizer/templates/" + retiredID, http.StatusNotFound},
		{"/api/customizer/templates/00000000-0000-4000-8000-000000000099", http.StatusNotFound},
		{"/api/customizer/templates/not-a-uuid", http.StatusNotFound},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.code {
			t.Fatalf("%s: status=%d want %d body=%s", tc.path, rec.Code, tc.code, rec.Body.String())
		}
		if tc.code == http.StatusNotFound {
			var errBody map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] != "not_found" {
				t.Fatalf("%s error json = %s", tc.path, rec.Body.String())
			}
		}
	}
}

func TestTemplates_NilCatalog_503(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	httpapi.ListTemplates(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/templates", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("list status=%d", rec.Code)
	}
	var errBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] != "unavailable" {
		t.Fatalf("list body=%s", rec.Body.String())
	}

	mux := http.NewServeMux()
	mux.Handle("GET /api/customizer/templates/{id}", httpapi.GetTemplate(nil))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/templates/11111111-1111-4111-8111-111111111111", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("get status=%d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] != "unavailable" {
		t.Fatalf("get body=%s", rec.Body.String())
	}
}

func TestTemplates_StoreError_500(t *testing.T) {
	t.Parallel()
	store := &tplMem{err: errors.New("db down")}
	cat := &application.TemplateCatalog{Store: store}
	rec := httptest.NewRecorder()
	httpapi.ListTemplates(cat).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/templates", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("list status=%d", rec.Code)
	}
	var errBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] != "internal_error" {
		t.Fatalf("list body=%s", rec.Body.String())
	}

	mux := http.NewServeMux()
	mux.Handle("GET /api/customizer/templates/{id}", httpapi.GetTemplate(cat))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/templates/11111111-1111-4111-8111-111111111111", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("get status=%d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] != "internal_error" {
		t.Fatalf("get body=%s", rec.Body.String())
	}
}
