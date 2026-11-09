package postgres_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestTemplateStore_LoadDraft_RoundTripAndVersionBump(t *testing.T) {
	db := testutil.OpenIsolated(t, "templates_store")
	store := postgres.NewTemplateStore(db)
	cat := &application.TemplateCatalog{Store: store, IDs: ports.UUIDGenerator{}}
	ctx := context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "head-front-blackmetal.yaml")
	body := minimalYAML("head-front-blackmetal", "prompt-a")
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}

	r1, err := cat.LoadDraft(ctx, path)
	if err != nil || !r1.Created || r1.Template.Version != 1 || r1.Template.Status != domain.TemplateDraft {
		t.Fatalf("first load: %+v err=%v", r1, err)
	}
	r2, err := cat.LoadDraft(ctx, path)
	if err != nil || r2.Created || r2.Template.Version != 1 {
		t.Fatalf("noop reload: %+v err=%v", r2, err)
	}

	if err := os.WriteFile(path, []byte(minimalYAML("head-front-blackmetal", "prompt-b")), 0o640); err != nil {
		t.Fatal(err)
	}
	r3, err := cat.LoadDraft(ctx, path)
	if err != nil || !r3.Created || r3.Template.Version != 2 {
		t.Fatalf("version bump: %+v err=%v", r3, err)
	}

	got, ok, err := store.GetBySlugVersion(ctx, "head-front-blackmetal", 2)
	if err != nil || !ok || got.PromptInstructions.Text != "prompt-b" {
		t.Fatalf("get v2: %+v ok=%v err=%v", got, ok, err)
	}

	pub, err := cat.Publish(ctx, "head-front-blackmetal", 0)
	if err != nil || pub.Status != domain.TemplatePublished || pub.Version != 2 {
		t.Fatalf("publish: %+v err=%v", pub, err)
	}
	if pub.PublishedAt == nil {
		t.Fatal("PublishedAt must be set after publish")
	}
	// Older draft is retired on publish so default publish cannot revive it.
	v1, ok, err := store.GetBySlugVersion(ctx, "head-front-blackmetal", 1)
	if err != nil || !ok || v1.Status != domain.TemplateRetired {
		t.Fatalf("v1 status after publish: %+v", v1)
	}

	retired, err := cat.Retire(ctx, "head-front-blackmetal")
	if err != nil || retired.Status != domain.TemplateRetired {
		t.Fatalf("retire: %+v err=%v", retired, err)
	}

	// Unchanged YAML after retire must create a new draft (not no-op on retired hash).
	r4, err := cat.LoadDraft(ctx, path)
	if err != nil || !r4.Created || r4.Template.Version != 3 || r4.Template.Status != domain.TemplateDraft {
		t.Fatalf("load after retire: %+v err=%v", r4, err)
	}
	pub2, err := cat.Publish(ctx, "head-front-blackmetal", 0)
	if err != nil || pub2.Version != 3 || pub2.Status != domain.TemplatePublished {
		t.Fatalf("republish after load: %+v err=%v", pub2, err)
	}
}

func TestTemplateStore_UniquePublishedIndex(t *testing.T) {
	db := testutil.OpenIsolated(t, "templates_unique_pub")
	store := postgres.NewTemplateStore(db)
	ids := ports.UUIDGenerator{}
	ctx := context.Background()

	mk := func(version int) domain.Template {
		t := domain.Template{
			ID: ids.New(), Slug: "uniq-pub", Status: domain.TemplateDraft, Version: version,
			Genre: "g", StyleCategory: "s", ContentHash: fmt.Sprintf("hash-%d", version),
			PromptInstructions: domain.PromptInstructions{Text: "p", PromptVersion: "v"},
			ModelParameters:    domain.ModelParameters{Model: "gpt-image-2.5-sunburst"},
			PhotoRequirements:  domain.PhotoRequirements{Pose: "head_front"},
			TextFields: domain.TextFields{
				Editable: []domain.EditableText{{Key: "dog_name", MaxLength: 8, AllowedCharset: "latin_polish"}},
				Fixed:    []domain.FixedText{},
			},
			OutputConfig: domain.OutputConfig{
				CanvasWidth: 64, CanvasHeight: 64, Monochrome: true,
				PrintArea: domain.PrintArea{Width: 64, Height: 64},
			},
		}
		return t
	}
	a := mk(1)
	b := mk(2)
	if err := store.InsertDraft(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertDraft(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := store.SetStatus(ctx, a.ID, domain.TemplatePublished); err != nil {
		t.Fatal(err)
	}
	err := store.SetStatus(ctx, b.ID, domain.TemplatePublished)
	if err == nil {
		t.Fatal("expected unique published-per-slug violation")
	}
	if !strings.Contains(err.Error(), "metaldogs_templates_one_published_per_slug") &&
		!strings.Contains(err.Error(), "unique") &&
		!strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func minimalYAML(slug, prompt string) string {
	return `slug: ` + slug + `
genre: black_metal
style_category: head_portrait
prompt_instructions:
  text: ` + prompt + `
  prompt_version: g2-test
model_parameters:
  model: gpt-image-2.5-sunburst
  width: 1024
  height: 1024
  seed: 1
photo_requirements:
  pose: head_front
text_fields:
  editable:
    - key: dog_name
      max_length: 12
      allowed_charset: latin_polish
  fixed: []
output_config:
  canvas_width: 1024
  canvas_height: 1024
  monochrome: true
  print_area: { x: 0, y: 0, width: 1024, height: 1024 }
`
}
