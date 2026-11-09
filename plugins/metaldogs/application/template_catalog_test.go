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
		return domain.Template{}, fmt.Errorf("missing id")
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
	now := time.Now().UTC()
	target.Status = domain.TemplatePublished
	target.PublishedAt = &now
	m.rows[id] = target
	return target, nil
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
	// inject escaping path
	raw, _ := os.ReadFile(path)
	raw = append(raw, []byte("\nreference_pattern_path: ../outside.png\n")...)
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := application.ValidatePath(path); err == nil {
		t.Fatal("expected path escape error")
	}
}
