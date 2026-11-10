package application_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

type memStore struct {
	mu   sync.Mutex
	rows map[string]domain.Template // id → row
}

func newMemStore() *memStore {
	return &memStore{rows: map[string]domain.Template{}}
}

func (m *memStore) list() []domain.Template {
	out := make([]domain.Template, 0, len(m.rows))
	for _, r := range m.rows {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Slug == out[j].Slug {
			return out[i].Version < out[j].Version
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

func (m *memStore) InsertDraft(_ context.Context, t domain.Template) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.Slug == t.Slug && r.Version == t.Version {
			return fmt.Errorf("insert: %w", ports.ErrTemplateVersionConflict)
		}
	}
	t.Status = domain.TemplateDraft
	m.rows[t.ID] = t
	return nil
}

func (m *memStore) LatestBySlug(_ context.Context, slug string) (domain.Template, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best domain.Template
	found := false
	for _, r := range m.rows {
		if r.Slug != slug {
			continue
		}
		if !found || r.Version > best.Version {
			best, found = r, true
		}
	}
	return best, found, nil
}

func (m *memStore) GetBySlugVersion(_ context.Context, slug string, version int) (domain.Template, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.Slug == slug && r.Version == version {
			return r, true, nil
		}
	}
	return domain.Template{}, false, nil
}

func (m *memStore) LatestDraftBySlug(_ context.Context, slug string) (domain.Template, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best domain.Template
	found := false
	for _, r := range m.rows {
		if r.Slug != slug || r.Status != domain.TemplateDraft {
			continue
		}
		if !found || r.Version > best.Version {
			best, found = r, true
		}
	}
	return best, found, nil
}

func (m *memStore) DraftBySlugHash(_ context.Context, slug, hash string) (domain.Template, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best domain.Template
	found := false
	for _, r := range m.rows {
		if r.Slug != slug || r.Status != domain.TemplateDraft || r.ContentHash != hash {
			continue
		}
		if !found || r.Version > best.Version {
			best, found = r, true
		}
	}
	return best, found, nil
}

func (m *memStore) PublishedBySlug(_ context.Context, slug string) (domain.Template, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.Slug == slug && r.Status == domain.TemplatePublished {
			return r, true, nil
		}
	}
	return domain.Template{}, false, nil
}

func (m *memStore) ListPublished(_ context.Context, genre string) ([]ports.PublishedTemplate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ports.PublishedTemplate
	for _, r := range m.rows {
		if r.Status != domain.TemplatePublished {
			continue
		}
		if genre != "" && r.Genre != genre {
			continue
		}
		out = append(out, ports.PublishedFromDomain(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	if out == nil {
		out = []ports.PublishedTemplate{}
	}
	return out, nil
}

func (m *memStore) GetPublishedByID(_ context.Context, id string) (ports.PublishedTemplate, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok || r.Status != domain.TemplatePublished {
		return ports.PublishedTemplate{}, false, nil
	}
	return ports.PublishedFromDomain(r), true, nil
}

func (m *memStore) GetPublicByID(_ context.Context, id string) (ports.PublishedTemplate, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return ports.PublishedTemplate{}, false, nil
	}
	return ports.PublishedFromDomain(r), true, nil
}

func (m *memStore) GetByID(_ context.Context, id string) (domain.Template, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return domain.Template{}, false, nil
	}
	return r, true, nil
}

func (m *memStore) SetStatus(_ context.Context, id, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return fmt.Errorf("missing id %s", id)
	}
	r.Status = status
	if status == domain.TemplatePublished {
		now := time.Now().UTC()
		r.PublishedAt = &now
	}
	m.rows[id] = r
	return nil
}

func (m *memStore) PublishExclusive(_ context.Context, slug, id string) (domain.Template, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	target, ok := m.rows[id]
	if !ok || target.Slug != slug {
		return domain.Template{}, fmt.Errorf("missing id/slug")
	}
	for k, r := range m.rows {
		if r.Slug != slug || r.ID == id {
			continue
		}
		if r.Status == domain.TemplatePublished || r.Status == domain.TemplateDraft {
			r.Status = domain.TemplateRetired
			m.rows[k] = r
		}
	}
	// Mirror SQL: keep stamp if already published; otherwise refresh.
	if target.Status != domain.TemplatePublished || target.PublishedAt == nil {
		now := time.Now().UTC()
		target.PublishedAt = &now
	}
	target.Status = domain.TemplatePublished
	m.rows[id] = target
	return target, nil
}

func (m *memStore) RetireExclusive(_ context.Context, slug string) (domain.Template, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var retired []domain.Template
	for k, r := range m.rows {
		if r.Slug != slug || r.Status != domain.TemplatePublished {
			continue
		}
		r.Status = domain.TemplateRetired
		m.rows[k] = r
		retired = append(retired, r)
	}
	if len(retired) == 0 {
		return domain.Template{}, fmt.Errorf("no published template for slug %q", slug)
	}
	for _, r := range m.rows {
		if r.Slug == slug && r.Status == domain.TemplatePublished {
			return domain.Template{}, fmt.Errorf("published remain after retire")
		}
	}
	best := retired[0]
	for _, r := range retired[1:] {
		if r.Version > best.Version {
			best = r
		}
	}
	return best, nil
}

var _ ports.TemplateStore = (*memStore)(nil)

type seqIDs struct{ n int }

func (s *seqIDs) New() string {
	s.n++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", s.n)
}

func writeYAML(t *testing.T, dir, slug, prompt string) string {
	t.Helper()
	path := filepath.Join(dir, slug+".yaml")
	body := `slug: ` + slug + `
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
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCatalog_LoadDraft_NoopAndBump(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	cat := &application.TemplateCatalog{Store: store, IDs: &seqIDs{}}
	ctx := context.Background()
	dir := t.TempDir()
	path := writeYAML(t, dir, "head-front-blackmetal", "a")

	r1, err := cat.LoadDraft(ctx, path)
	if err != nil || !r1.Created || r1.Template.Version != 1 {
		t.Fatalf("r1=%+v err=%v", r1, err)
	}
	r2, err := cat.LoadDraft(ctx, path)
	if err != nil || r2.Created {
		t.Fatalf("noop expected: %+v err=%v", r2, err)
	}
	path = writeYAML(t, dir, "head-front-blackmetal", "b")
	r3, err := cat.LoadDraft(ctx, path)
	if err != nil || !r3.Created || r3.Template.Version != 2 {
		t.Fatalf("bump=%+v err=%v", r3, err)
	}
}

func TestCatalog_PublishDefault_RetiresOlderDrafts(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	cat := &application.TemplateCatalog{Store: store, IDs: &seqIDs{}}
	ctx := context.Background()
	dir := t.TempDir()
	path := writeYAML(t, dir, "x", "a")
	if _, err := cat.LoadDraft(ctx, path); err != nil {
		t.Fatal(err)
	}
	path = writeYAML(t, dir, "x", "b")
	if _, err := cat.LoadDraft(ctx, path); err != nil {
		t.Fatal(err)
	}
	pub, err := cat.Publish(ctx, "x", 0)
	if err != nil || pub.Version != 2 {
		t.Fatalf("pub=%+v err=%v", pub, err)
	}
	v1, ok, err := store.GetBySlugVersion(ctx, "x", 1)
	if err != nil || !ok || v1.Status != domain.TemplateRetired {
		t.Fatalf("v1=%+v", v1)
	}
	if _, err := cat.Publish(ctx, "x", 0); err == nil {
		t.Fatal("expected no draft after publish")
	}
}

func TestCatalog_LoadAfterRetire_CreatesDraft(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	cat := &application.TemplateCatalog{Store: store, IDs: &seqIDs{}}
	ctx := context.Background()
	dir := t.TempDir()
	path := writeYAML(t, dir, "y", "same")
	if _, err := cat.LoadDraft(ctx, path); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Publish(ctx, "y", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Retire(ctx, "y"); err != nil {
		t.Fatal(err)
	}
	r, err := cat.LoadDraft(ctx, path)
	if err != nil || !r.Created || r.Template.Version != 2 {
		t.Fatalf("after retire load=%+v err=%v", r, err)
	}
	pub, err := cat.Publish(ctx, "y", 0)
	if err != nil || pub.Version != 2 {
		t.Fatalf("republish=%+v err=%v", pub, err)
	}
}

func TestCatalog_PublishExplicitRetiredVersion(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	cat := &application.TemplateCatalog{Store: store, IDs: &seqIDs{}}
	ctx := context.Background()
	dir := t.TempDir()
	path := writeYAML(t, dir, "z", "a")
	if _, err := cat.LoadDraft(ctx, path); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Publish(ctx, "z", 0); err != nil {
		t.Fatal(err)
	}
	path = writeYAML(t, dir, "z", "b")
	if _, err := cat.LoadDraft(ctx, path); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Publish(ctx, "z", 0); err != nil {
		t.Fatal(err)
	}
	// v1 was retired by second publish; roll back via -version
	pub, err := cat.Publish(ctx, "z", 1)
	if err != nil || pub.Version != 1 || pub.Status != domain.TemplatePublished {
		t.Fatalf("rollback=%+v err=%v", pub, err)
	}
	v2, ok, _ := store.GetBySlugVersion(ctx, "z", 2)
	if !ok || v2.Status != domain.TemplateRetired {
		t.Fatalf("v2 should be retired: %+v", v2)
	}
}

func TestCatalog_PublishAlreadyPublished_RetiresLeftoverDraft(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	cat := &application.TemplateCatalog{Store: store, IDs: &seqIDs{}}
	ctx := context.Background()
	dir := t.TempDir()
	path := writeYAML(t, dir, "same", "identical")
	if _, err := cat.LoadDraft(ctx, path); err != nil {
		t.Fatal(err)
	}
	pub, err := cat.Publish(ctx, "same", 0)
	if err != nil || pub.Version != 1 {
		t.Fatalf("pub=%+v err=%v", pub, err)
	}
	// Same YAML → new draft (hash matches published, not a draft).
	r, err := cat.LoadDraft(ctx, path)
	if err != nil || !r.Created || r.Template.Version != 2 {
		t.Fatalf("load after publish=%+v err=%v", r, err)
	}
	// Explicit publish of already-published v1 must clean leftover draft, not bump.
	again, err := cat.Publish(ctx, "same", 1)
	if err != nil || again.Version != 1 || again.Status != domain.TemplatePublished {
		t.Fatalf("re-publish published=%+v err=%v", again, err)
	}
	v2, ok, err := store.GetBySlugVersion(ctx, "same", 2)
	if err != nil || !ok || v2.Status != domain.TemplateRetired {
		t.Fatalf("leftover draft must be retired: %+v", v2)
	}
	if _, err := cat.Publish(ctx, "same", 0); err == nil {
		t.Fatal("bare publish must fail with no drafts left")
	}
}

func TestCatalog_LoadDraft_RetriesVersionConflict(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	// Pre-seed version 1 so first attempt with version=1 conflicts when raced... simpler:
	// force first InsertDraft to conflict by occupying chosen version via hook.
	ids := &seqIDs{}
	cat := &application.TemplateCatalog{Store: store, IDs: ids}
	ctx := context.Background()
	dir := t.TempDir()
	pathA := writeYAML(t, dir, "race", "a")
	if _, err := cat.LoadDraft(ctx, pathA); err != nil {
		t.Fatal(err)
	}
	// Simulate concurrent loser: LatestBySlug still sees v1 while v2 already inserted mid-flight.
	// Occupy the version the next load would pick (2) before second load starts? After load A, latest=1.
	// Insert a conflicting row at version 2 with different id while LoadDraft B computes version=2.
	blocker := domain.Template{
		ID: "00000000-0000-4000-8000-000000000099", Slug: "race", Version: 2, Status: domain.TemplateDraft,
		ContentHash: "blocker", Genre: "g", StyleCategory: "s",
		PromptInstructions: domain.PromptInstructions{Text: "x", PromptVersion: "v"},
		ModelParameters:    domain.ModelParameters{Model: "m"},
		PhotoRequirements:  domain.PhotoRequirements{Pose: "head_front"},
		TextFields: domain.TextFields{
			Editable: []domain.EditableText{{Key: "dog_name", MaxLength: 8, AllowedCharset: "latin_polish"}},
			Fixed:    []domain.FixedText{},
		},
		OutputConfig: domain.OutputConfig{CanvasWidth: 64, CanvasHeight: 64, PrintArea: domain.PrintArea{Width: 64, Height: 64}},
	}
	if err := store.InsertDraft(ctx, blocker); err != nil {
		t.Fatal(err)
	}
	pathB := writeYAML(t, dir, "race", "b")
	r, err := cat.LoadDraft(ctx, pathB)
	if err != nil || !r.Created || r.Template.Version != 3 {
		t.Fatalf("retry should land on v3: %+v err=%v", r, err)
	}
}

func TestCatalog_RejectTrailingYAMLDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.yaml")
	body := writeYAML(t, dir, "first", "a")
	raw, _ := os.ReadFile(body)
	raw = append(raw, []byte("\n---\nslug: second\n")...)
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := application.ParseFile(path); err == nil {
		t.Fatal("expected trailing document error")
	}
}

func TestCatalog_RejectUnknownYAMLField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	body := `slug: bad
genre: g
style_category: s
prompt_instruction:
  text: typo key
  prompt_version: v
model_parameters:
  model: m
photo_requirements:
  pose: head_front
text_fields:
  editable:
    - key: dog_name
      max_length: 1
      allowed_charset: latin_polish
output_config:
  canvas_width: 64
  canvas_height: 64
  monochrome: true
  print_area: { x: 0, y: 0, width: 64, height: 64 }
`
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := application.ValidatePath(path); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestCatalog_RejectPathEscape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeYAML(t, dir, "esc", "p")
	raw, _ := os.ReadFile(path)
	raw = append(raw, []byte("\nreference_pattern_path: ../outside.png\n")...)
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := application.ValidatePath(path); err == nil {
		t.Fatal("expected path escape error")
	}
}

func TestCatalog_AllowsDotDotPrefixedFilename(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	asset := filepath.Join(dir, "..assets.png")
	if err := os.WriteFile(asset, []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	path := writeYAML(t, dir, "dotdotname", "p")
	raw, _ := os.ReadFile(path)
	raw = append(raw, []byte("\nreference_pattern_path: ..assets.png\n")...)
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := application.ValidatePath(path); err != nil {
		t.Fatalf("..assets.png under template dir must be allowed: %v", err)
	}
}
