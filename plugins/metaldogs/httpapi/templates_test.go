package httpapi_test

import (
	"context"
	"encoding/json"
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
}

func (m *tplMem) InsertDraft(context.Context, domain.Template) error {
	return nil
}
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

func (m *tplMem) ListPublished(_ context.Context, genre string) ([]domain.Template, error) {
	var out []domain.Template
	for _, r := range m.rows {
		if r.Status != domain.TemplatePublished {
			continue
		}
		if genre != "" && r.Genre != genre {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (m *tplMem) GetPublishedByID(_ context.Context, id string) (domain.Template, bool, error) {
	r, ok := m.rows[id]
	if !ok || r.Status != domain.TemplatePublished {
		return domain.Template{}, false, nil
	}
	return r, true, nil
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

func TestListTemplates_DTOExcludesSecretsAndDrafts(t *testing.T) {
	t.Parallel()
	pubID := "11111111-1111-4111-8111-111111111111"
	draftID := "22222222-2222-4222-8222-222222222222"
	store := &tplMem{rows: map[string]domain.Template{
		pubID: samplePublished(pubID, "head-front-blackmetal", "black_metal"),
		draftID: func() domain.Template {
			d := samplePublished(draftID, "drafty", "punk")
			d.Status = domain.TemplateDraft
			return d
		}(),
	}}
	cat := &application.TemplateCatalog{Store: store}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/customizer/templates", nil)
	httpapi.ListTemplatesWithCatalog(cat).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	if strings.Contains(raw, "SECRET PROMPT") || strings.Contains(raw, "secret-model") ||
		strings.Contains(raw, "content_hash") || strings.Contains(raw, "prompt_instructions") ||
		strings.Contains(raw, "model_parameters") || strings.Contains(raw, "reference_pattern") ||
		strings.Contains(raw, "thumbnail_path") {
		t.Fatalf("DTO leaked secrets: %s", raw)
	}
	if strings.Contains(raw, "drafty") {
		t.Fatal("draft must not appear in list")
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
	httpapi.ListTemplatesWithCatalog(cat).ServeHTTP(rec, req)
	var body struct {
		Templates []httpapi.PublicTemplate `json:"templates"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Templates) != 1 || body.Templates[0].Slug != "b" {
		t.Fatalf("body = %+v", body)
	}
}

func TestGetTemplate_404ForDraftUnknownAndBadID(t *testing.T) {
	t.Parallel()
	pubID := "11111111-1111-4111-8111-111111111111"
	draftID := "22222222-2222-4222-8222-222222222222"
	store := &tplMem{rows: map[string]domain.Template{
		pubID: samplePublished(pubID, "pub", "punk"),
		draftID: func() domain.Template {
			d := samplePublished(draftID, "draft", "punk")
			d.Status = domain.TemplateDraft
			return d
		}(),
	}}
	cat := &application.TemplateCatalog{Store: store}
	h := httpapi.GetTemplateWithCatalog(cat)

	mux := http.NewServeMux()
	mux.Handle("GET /api/customizer/templates/{id}", h)

	cases := []struct {
		path string
		code int
	}{
		{"/api/customizer/templates/" + pubID, http.StatusOK},
		{"/api/customizer/templates/" + draftID, http.StatusNotFound},
		{"/api/customizer/templates/00000000-0000-4000-8000-000000000099", http.StatusNotFound},
		{"/api/customizer/templates/not-a-uuid", http.StatusNotFound},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.code {
			t.Fatalf("%s: status=%d want %d body=%s", tc.path, rec.Code, tc.code, rec.Body.String())
		}
	}
}
