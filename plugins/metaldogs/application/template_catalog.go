// Package application coordinates Metal Dogs use cases.
package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"gopkg.in/yaml.v3"
)

const loadDraftMaxAttempts = 5

// TemplateCatalog loads and publishes versioned templates.
type TemplateCatalog struct {
	Store ports.TemplateStore
	IDs   ports.IDGenerator
}

// ParseFile reads a Template v1 YAML or JSON definition from path (unknown fields rejected).
func ParseFile(path string) (domain.FileDefinition, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.FileDefinition{}, fmt.Errorf("template catalog: read: %w", err)
	}
	var def domain.FileDefinition
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if err := dec.Decode(&def); err != nil {
			return domain.FileDefinition{}, fmt.Errorf("template catalog: parse yaml: %w", err)
		}
		var extra yaml.Node
		if err := dec.Decode(&extra); err != io.EOF {
			if err == nil {
				return domain.FileDefinition{}, fmt.Errorf("template catalog: trailing YAML document after first template")
			}
			return domain.FileDefinition{}, fmt.Errorf("template catalog: parse yaml: %w", err)
		}
	case ".json":
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&def); err != nil {
			return domain.FileDefinition{}, fmt.Errorf("template catalog: parse json: %w", err)
		}
		if dec.More() {
			return domain.FileDefinition{}, fmt.Errorf("template catalog: trailing JSON after first template")
		}
	default:
		return domain.FileDefinition{}, fmt.Errorf("template catalog: unsupported extension %q (want .yaml/.yml/.json)", filepath.Ext(path))
	}
	return def, nil
}

// ValidatePath parses and validates a template file; optional asset paths must stay under the file dir.
func ValidatePath(path string) (domain.Template, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return domain.Template{}, fmt.Errorf("template catalog: abs path: %w", err)
	}
	def, err := ParseFile(absPath)
	if err != nil {
		return domain.Template{}, err
	}
	t := def.ToTemplate()
	if err := t.Validate(); err != nil {
		return domain.Template{}, err
	}
	base := filepath.Dir(absPath)
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
	if filepath.IsAbs(rel) {
		return fmt.Errorf("template catalog: asset path %q must be relative to the template file", rel)
	}
	joined := filepath.Join(base, rel)
	clean := filepath.Clean(joined)
	relToBase, err := filepath.Rel(base, clean)
	if err != nil || pathEscapes(relToBase) {
		return fmt.Errorf("template catalog: asset path %q escapes template directory", rel)
	}
	st, err := os.Stat(clean)
	if err != nil {
		return fmt.Errorf("template catalog: asset path %q: %w", rel, err)
	}
	if st.IsDir() {
		return fmt.Errorf("template catalog: asset path %q is a directory", rel)
	}
	return nil
}

func pathEscapes(rel string) bool {
	if rel == ".." {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(rel, ".."+sep)
}

// LoadResult describes a LoadDraft outcome.
type LoadResult struct {
	Template domain.Template
	Created  bool // false when a draft with the same content_hash already exists
}

// LoadDraft validates the file and inserts a new draft when no draft matches the content hash.
// Matching published/retired rows do not count as a no-op (a new draft is created so publish can proceed).
// Concurrent loads that collide on UNIQUE (slug, version) are retried.
func (c *TemplateCatalog) LoadDraft(ctx context.Context, path string) (LoadResult, error) {
	if c == nil || c.Store == nil || c.IDs == nil {
		return LoadResult{}, fmt.Errorf("template catalog: store and id generator required")
	}
	t, err := ValidatePath(path)
	if err != nil {
		return LoadResult{}, err
	}
	for attempt := 0; attempt < loadDraftMaxAttempts; attempt++ {
		existing, ok, err := c.Store.DraftBySlugHash(ctx, t.Slug, t.ContentHash)
		if err != nil {
			return LoadResult{}, err
		}
		if ok {
			return LoadResult{Template: existing, Created: false}, nil
		}
		latest, okLatest, err := c.Store.LatestBySlug(ctx, t.Slug)
		if err != nil {
			return LoadResult{}, err
		}
		version := 1
		if okLatest {
			version = latest.Version + 1
		}
		row := t
		row.ID = c.IDs.New()
		row.Version = version
		row.Status = domain.TemplateDraft
		if err := c.Store.InsertDraft(ctx, row); err != nil {
			if errors.Is(err, ports.ErrTemplateVersionConflict) {
				continue
			}
			return LoadResult{}, err
		}
		return LoadResult{Template: row, Created: true}, nil
	}
	return LoadResult{}, fmt.Errorf("template catalog: version conflict for slug %q after %d attempts", t.Slug, loadDraftMaxAttempts)
}

// Publish promotes a draft (latest draft when version=0, or an explicit version) to published.
// Explicit --version may re-publish a retired row. Other drafts for the slug are retired atomically.
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
		return domain.Template{}, fmt.Errorf("template catalog: version not found for slug %q (load a draft first, or pass -version)", slug)
	}
	switch t.Status {
	case domain.TemplatePublished:
		// Still run exclusive cleanup so leftover drafts from a same-hash LoadDraft are retired.
		return c.Store.PublishExclusive(ctx, slug, t.ID)
	case domain.TemplateDraft:
		return c.Store.PublishExclusive(ctx, slug, t.ID)
	case domain.TemplateRetired:
		if version <= 0 {
			return domain.Template{}, fmt.Errorf("template catalog: cannot publish status %q (want draft)", t.Status)
		}
		return c.Store.PublishExclusive(ctx, slug, t.ID)
	default:
		return domain.Template{}, fmt.Errorf("template catalog: cannot publish status %q", t.Status)
	}
}

// Retire marks every published template for slug as retired (exclusive; fails if none published).
func (c *TemplateCatalog) Retire(ctx context.Context, slug string) (domain.Template, error) {
	if c == nil || c.Store == nil {
		return domain.Template{}, fmt.Errorf("template catalog: store required")
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return domain.Template{}, fmt.Errorf("template catalog: slug required")
	}
	return c.Store.RetireExclusive(ctx, slug)
}
