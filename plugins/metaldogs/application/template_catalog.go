// Package application coordinates Metal Dogs use cases.
package application

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"gopkg.in/yaml.v3"
)

// TemplateCatalog loads and publishes versioned templates.
type TemplateCatalog struct {
	Store ports.TemplateStore
	IDs   ports.IDGenerator
}

// ParseFile reads a Template v1 YAML or JSON definition from path.
func ParseFile(path string) (domain.FileDefinition, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.FileDefinition{}, fmt.Errorf("template catalog: read: %w", err)
	}
	var def domain.FileDefinition
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(raw, &def); err != nil {
			return domain.FileDefinition{}, fmt.Errorf("template catalog: parse yaml: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(raw, &def); err != nil {
			return domain.FileDefinition{}, fmt.Errorf("template catalog: parse json: %w", err)
		}
	default:
		return domain.FileDefinition{}, fmt.Errorf("template catalog: unsupported extension %q (want .yaml/.yml/.json)", filepath.Ext(path))
	}
	return def, nil
}

// ValidatePath parses and validates a template file; optional asset paths must exist relative to the file dir.
func ValidatePath(path string) (domain.Template, error) {
	def, err := ParseFile(path)
	if err != nil {
		return domain.Template{}, err
	}
	t := def.ToTemplate()
	if err := t.Validate(); err != nil {
		return domain.Template{}, err
	}
	base := filepath.Dir(path)
	if err := checkOptionalPath(base, t.ReferencePatternPath); err != nil {
		return domain.Template{}, err
	}
	if err := checkOptionalPath(base, t.ThumbnailPath); err != nil {
		return domain.Template{}, err
	}
	hash, err := t.ContentHashDigest()
	if err != nil {
		return domain.Template{}, err
	}
	t.ContentHash = hash
	return t, nil
}

func checkOptionalPath(base, rel string) error {
	if rel == "" {
		return nil
	}
	abs := rel
	if !filepath.IsAbs(rel) {
		abs = filepath.Join(base, rel)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("template catalog: asset path %q: %w", rel, err)
	}
	if st.IsDir() {
		return fmt.Errorf("template catalog: asset path %q is a directory", rel)
	}
	return nil
}

// LoadResult describes a LoadDraft outcome.
type LoadResult struct {
	Template domain.Template
	Created  bool // false when content_hash matched latest row (no-op)
}

// LoadDraft validates the file and inserts a new draft version when the content hash changed.
func (c *TemplateCatalog) LoadDraft(ctx context.Context, path string) (LoadResult, error) {
	if c == nil || c.Store == nil || c.IDs == nil {
		return LoadResult{}, fmt.Errorf("template catalog: store and id generator required")
	}
	t, err := ValidatePath(path)
	if err != nil {
		return LoadResult{}, err
	}
	latest, ok, err := c.Store.LatestBySlug(ctx, t.Slug)
	if err != nil {
		return LoadResult{}, err
	}
	if ok && latest.ContentHash == t.ContentHash {
		return LoadResult{Template: latest, Created: false}, nil
	}
	version := 1
	if ok {
		version = latest.Version + 1
	}
	t.ID = c.IDs.New()
	t.Version = version
	t.Status = domain.TemplateDraft
	if err := c.Store.InsertDraft(ctx, t); err != nil {
		return LoadResult{}, err
	}
	return LoadResult{Template: t, Created: true}, nil
}

// Publish promotes a draft (latest draft, or specific version) to published and retires prior published.
func (c *TemplateCatalog) Publish(ctx context.Context, slug string, version int) (domain.Template, error) {
	if c == nil || c.Store == nil {
		return domain.Template{}, fmt.Errorf("template catalog: store required")
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return domain.Template{}, fmt.Errorf("template catalog: slug required")
	}
	var (
		t   domain.Template
		ok  bool
		err error
	)
	if version > 0 {
		t, ok, err = c.Store.GetBySlugVersion(ctx, slug, version)
	} else {
		t, ok, err = c.Store.LatestDraftBySlug(ctx, slug)
	}
	if err != nil {
		return domain.Template{}, err
	}
	if !ok {
		return domain.Template{}, fmt.Errorf("template catalog: version not found for slug %q", slug)
	}
	if t.Status == domain.TemplatePublished {
		return t, nil
	}
	// Latest-draft path requires draft; explicit --version may re-publish a retired row (runbook rollback).
	if version <= 0 && t.Status != domain.TemplateDraft {
		return domain.Template{}, fmt.Errorf("template catalog: cannot publish status %q (want draft)", t.Status)
	}
	if version > 0 && t.Status != domain.TemplateDraft && t.Status != domain.TemplateRetired {
		return domain.Template{}, fmt.Errorf("template catalog: cannot publish status %q", t.Status)
	}
	if err := c.Store.RetirePublishedExcept(ctx, slug, t.ID); err != nil {
		return domain.Template{}, err
	}
	if err := c.Store.SetStatus(ctx, t.ID, domain.TemplatePublished); err != nil {
		return domain.Template{}, err
	}
	t.Status = domain.TemplatePublished
	return t, nil
}

// Retire marks the published template for slug as retired.
func (c *TemplateCatalog) Retire(ctx context.Context, slug string) (domain.Template, error) {
	if c == nil || c.Store == nil {
		return domain.Template{}, fmt.Errorf("template catalog: store required")
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return domain.Template{}, fmt.Errorf("template catalog: slug required")
	}
	t, ok, err := c.Store.PublishedBySlug(ctx, slug)
	if err != nil {
		return domain.Template{}, err
	}
	if !ok {
		return domain.Template{}, fmt.Errorf("template catalog: no published template for slug %q", slug)
	}
	if err := c.Store.SetStatus(ctx, t.ID, domain.TemplateRetired); err != nil {
		return domain.Template{}, err
	}
	t.Status = domain.TemplateRetired
	return t, nil
}
