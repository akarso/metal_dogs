// Package domain holds Metal Dogs domain types (G2+).
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Template status values (snake_case).
const (
	TemplateDraft     = "draft"
	TemplatePublished = "published"
	TemplateRetired   = "retired"
)

// MaxCanvasDimension caps template canvas size (aligned with generation.MaxDimension).
const MaxCanvasDimension = 2048

// Allowed photo poses for Template.PhotoRequirements.Pose.
var AllowedPoses = map[string]struct{}{
	"head_front":        {},
	"head_angle":        {},
	"full_body_profile": {},
	"full_body_front":   {},
	"other":             {},
}

// AllowedCharsets for editable text fields.
var AllowedCharsets = map[string]struct{}{
	"latin_polish": {},
	"latin":        {},
	"ascii":        {},
}

var slugRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Template is a versioned catalog definition (file ↔ DB).
type Template struct {
	ID                   string
	Slug                 string
	Status               string
	Genre                string
	StyleCategory        string
	Version              int
	ContentHash          string
	PromptInstructions   PromptInstructions
	ModelParameters      ModelParameters
	PhotoRequirements    PhotoRequirements
	TextFields           TextFields
	OutputConfig         OutputConfig
	ReferenceAssetID     string // optional UUID; empty until asset ingest
	ThumbnailAssetID     string
	ReferencePatternPath string // file-relative; not an asset id
	ThumbnailPath        string
	CreatedAt            time.Time
	PublishedAt          *time.Time
}

// PromptInstructions are server-only generation instructions.
type PromptInstructions struct {
	Text          string `json:"text" yaml:"text"`
	PromptVersion string `json:"prompt_version" yaml:"prompt_version"`
}

// ModelParameters are server-only model knobs.
type ModelParameters struct {
	Model  string `json:"model" yaml:"model"`
	Width  int    `json:"width" yaml:"width"`
	Height int    `json:"height" yaml:"height"`
	Seed   int64  `json:"seed" yaml:"seed"`
}

// PhotoRequirements describe the expected customer photo.
type PhotoRequirements struct {
	Pose           string `json:"pose" yaml:"pose"`
	BackgroundHint string `json:"background_hint" yaml:"background_hint"`
	LightingHint   string `json:"lighting_hint" yaml:"lighting_hint"`
}

// TextFields holds editable and fixed text definitions.
type TextFields struct {
	Editable []EditableText `json:"editable" yaml:"editable"`
	Fixed    []FixedText    `json:"fixed" yaml:"fixed"`
}

// EditableText is a customer-editable field (e.g. dog_name).
type EditableText struct {
	Key            string `json:"key" yaml:"key"`
	MaxLength      int    `json:"max_length" yaml:"max_length"`
	AllowedCharset string `json:"allowed_charset" yaml:"allowed_charset"`
}

// FixedText is non-editable template copy.
type FixedText struct {
	Key   string `json:"key" yaml:"key"`
	Value string `json:"value" yaml:"value"`
}

// OutputConfig describes canvas / print / monochrome.
type OutputConfig struct {
	CanvasWidth  int       `json:"canvas_width" yaml:"canvas_width"`
	CanvasHeight int       `json:"canvas_height" yaml:"canvas_height"`
	Monochrome   bool      `json:"monochrome" yaml:"monochrome"`
	PrintArea    PrintArea `json:"print_area" yaml:"print_area"`
}

// PrintArea is the print rectangle in canvas pixels.
type PrintArea struct {
	X      int `json:"x" yaml:"x"`
	Y      int `json:"y" yaml:"y"`
	Width  int `json:"width" yaml:"width"`
	Height int `json:"height" yaml:"height"`
}

// FileOutputConfig is the on-disk output_config shape. Monochrome uses a pointer
// so omitted keys default to true (match legacy eval stubs); explicit false is kept.
type FileOutputConfig struct {
	CanvasWidth  int       `json:"canvas_width" yaml:"canvas_width"`
	CanvasHeight int       `json:"canvas_height" yaml:"canvas_height"`
	Monochrome   *bool     `json:"monochrome" yaml:"monochrome"`
	PrintArea    PrintArea `json:"print_area" yaml:"print_area"`
}

// FileDefinition is the on-disk YAML/JSON shape (no status/version/id).
type FileDefinition struct {
	Slug                 string             `json:"slug" yaml:"slug"`
	Genre                string             `json:"genre" yaml:"genre"`
	StyleCategory        string             `json:"style_category" yaml:"style_category"`
	PromptInstructions   PromptInstructions `json:"prompt_instructions" yaml:"prompt_instructions"`
	ModelParameters      ModelParameters    `json:"model_parameters" yaml:"model_parameters"`
	PhotoRequirements    PhotoRequirements  `json:"photo_requirements" yaml:"photo_requirements"`
	TextFields           TextFields         `json:"text_fields" yaml:"text_fields"`
	OutputConfig         FileOutputConfig   `json:"output_config" yaml:"output_config"`
	ReferencePatternPath string             `json:"reference_pattern_path,omitempty" yaml:"reference_pattern_path,omitempty"`
	ThumbnailPath        string             `json:"thumbnail_path,omitempty" yaml:"thumbnail_path,omitempty"`
}

// ToTemplate builds an unsaved Template from a file definition.
func (d FileDefinition) ToTemplate() Template {
	mono := true
	if d.OutputConfig.Monochrome != nil {
		mono = *d.OutputConfig.Monochrome
	}
	t := Template{
		Slug:                 strings.TrimSpace(d.Slug),
		Genre:                strings.TrimSpace(d.Genre),
		StyleCategory:        strings.TrimSpace(d.StyleCategory),
		PromptInstructions:   d.PromptInstructions,
		ModelParameters:      d.ModelParameters,
		PhotoRequirements:    d.PhotoRequirements,
		TextFields:           d.TextFields,
		OutputConfig: OutputConfig{
			CanvasWidth:  d.OutputConfig.CanvasWidth,
			CanvasHeight: d.OutputConfig.CanvasHeight,
			Monochrome:   mono,
			PrintArea:    d.OutputConfig.PrintArea,
		},
		ReferencePatternPath: strings.TrimSpace(d.ReferencePatternPath),
		ThumbnailPath:        strings.TrimSpace(d.ThumbnailPath),
		Status:               TemplateDraft,
	}
	t.NormalizeTextKeys()
	t.NormalizeForHash()
	return t
}

// NormalizeForHash ensures nil slices become empty so omitted vs [] hash identically.
func (t *Template) NormalizeForHash() {
	if t.TextFields.Editable == nil {
		t.TextFields.Editable = []EditableText{}
	}
	if t.TextFields.Fixed == nil {
		t.TextFields.Fixed = []FixedText{}
	}
}

// NormalizeTextKeys trims editable/fixed keys so Validate and persistence agree.
func (t *Template) NormalizeTextKeys() {
	for i := range t.TextFields.Editable {
		t.TextFields.Editable[i].Key = strings.TrimSpace(t.TextFields.Editable[i].Key)
	}
	for i := range t.TextFields.Fixed {
		t.TextFields.Fixed[i].Key = strings.TrimSpace(t.TextFields.Fixed[i].Key)
	}
}

// Validate checks domain invariants for a template definition (status optional).
func (t Template) Validate() error {
	if !slugRE.MatchString(t.Slug) {
		return fmt.Errorf("template: invalid slug %q (want lowercase kebab-case)", t.Slug)
	}
	if strings.TrimSpace(t.Genre) == "" {
		return fmt.Errorf("template: genre required")
	}
	if strings.TrimSpace(t.StyleCategory) == "" {
		return fmt.Errorf("template: style_category required")
	}
	if strings.TrimSpace(t.PromptInstructions.Text) == "" {
		return fmt.Errorf("template: prompt_instructions.text required")
	}
	if strings.TrimSpace(t.PromptInstructions.PromptVersion) == "" {
		return fmt.Errorf("template: prompt_instructions.prompt_version required")
	}
	if strings.TrimSpace(t.ModelParameters.Model) == "" {
		return fmt.Errorf("template: model_parameters.model required")
	}
	if _, ok := AllowedPoses[t.PhotoRequirements.Pose]; !ok {
		return fmt.Errorf("template: unknown photo pose %q", t.PhotoRequirements.Pose)
	}
	if t.OutputConfig.CanvasWidth <= 0 || t.OutputConfig.CanvasHeight <= 0 {
		return fmt.Errorf("template: output_config canvas dimensions required")
	}
	if t.OutputConfig.CanvasWidth > MaxCanvasDimension || t.OutputConfig.CanvasHeight > MaxCanvasDimension {
		return fmt.Errorf("template: output_config canvas exceeds max %d", MaxCanvasDimension)
	}
	w, h := t.ModelParameters.Width, t.ModelParameters.Height
	cw, ch := t.OutputConfig.CanvasWidth, t.OutputConfig.CanvasHeight
	switch {
	case w == 0 && h == 0:
		// Omitted: adapters/eval request at output_config canvas size.
	case w > 0 && h > 0:
		if w > MaxCanvasDimension || h > MaxCanvasDimension {
			return fmt.Errorf("template: model_parameters width/height must be <= %d (got %dx%d)", MaxCanvasDimension, w, h)
		}
		if w != cw || h != ch {
			return fmt.Errorf("template: model_parameters width/height must match output_config canvas (got %dx%d, canvas %dx%d)", w, h, cw, ch)
		}
	default:
		return fmt.Errorf("template: model_parameters width and height must both be set or both omitted (got %dx%d)", w, h)
	}
	pa := t.OutputConfig.PrintArea
	if pa.Width <= 0 || pa.Height <= 0 {
		return fmt.Errorf("template: print_area width/height required")
	}
	if pa.X < 0 || pa.Y < 0 || pa.X+pa.Width > t.OutputConfig.CanvasWidth || pa.Y+pa.Height > t.OutputConfig.CanvasHeight {
		return fmt.Errorf("template: print_area outside canvas")
	}
	seenKeys := map[string]struct{}{}
	hasDogName := false
	for _, e := range t.TextFields.Editable {
		// Keys must already be trimmed (NormalizeTextKeys); reject padded leftovers.
		if e.Key != strings.TrimSpace(e.Key) {
			return fmt.Errorf("template: editable key %q has surrounding whitespace (normalize before Validate)", e.Key)
		}
		key := e.Key
		if key == "" {
			return fmt.Errorf("template: editable text key required")
		}
		if _, dup := seenKeys[key]; dup {
			return fmt.Errorf("template: duplicate editable key %q", key)
		}
		seenKeys[key] = struct{}{}
		if e.MaxLength <= 0 {
			return fmt.Errorf("template: editable %q max_length must be positive", key)
		}
		if _, ok := AllowedCharsets[e.AllowedCharset]; !ok {
			return fmt.Errorf("template: editable %q unknown allowed_charset %q", key, e.AllowedCharset)
		}
		if key == "dog_name" {
			hasDogName = true
		}
	}
	if !hasDogName {
		return fmt.Errorf("template: editable text_fields must include dog_name")
	}
	for _, f := range t.TextFields.Fixed {
		if f.Key != strings.TrimSpace(f.Key) {
			return fmt.Errorf("template: fixed key %q has surrounding whitespace (normalize before Validate)", f.Key)
		}
		if f.Key == "" || f.Value == "" {
			return fmt.Errorf("template: fixed text key and value required")
		}
	}
	if t.Status != "" {
		switch t.Status {
		case TemplateDraft, TemplatePublished, TemplateRetired:
		default:
			return fmt.Errorf("template: invalid status %q", t.Status)
		}
	}
	return nil
}

// hashPayload is the canonical subset hashed for content_hash (excludes id/status/version/timestamps/asset ids).
type hashPayload struct {
	Slug                 string             `json:"slug"`
	Genre                string             `json:"genre"`
	StyleCategory        string             `json:"style_category"`
	PromptInstructions   PromptInstructions `json:"prompt_instructions"`
	ModelParameters      ModelParameters    `json:"model_parameters"`
	PhotoRequirements    PhotoRequirements  `json:"photo_requirements"`
	TextFields           TextFields         `json:"text_fields"`
	OutputConfig         OutputConfig       `json:"output_config"`
	ReferencePatternPath string             `json:"reference_pattern_path"`
	ThumbnailPath        string             `json:"thumbnail_path"`
}

// ContentHashDigest returns a stable SHA-256 hex digest of the definition body.
func (t Template) ContentHashDigest() (string, error) {
	t.NormalizeForHash()
	payload := hashPayload{
		Slug:                 t.Slug,
		Genre:                t.Genre,
		StyleCategory:        t.StyleCategory,
		PromptInstructions:   t.PromptInstructions,
		ModelParameters:      t.ModelParameters,
		PhotoRequirements:    t.PhotoRequirements,
		TextFields:           t.TextFields,
		OutputConfig:         t.OutputConfig,
		ReferencePatternPath: t.ReferencePatternPath,
		ThumbnailPath:        t.ThumbnailPath,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("template: hash marshal: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// GenerationWidthHeight returns the request size for eval/adapters: model WxH when
// set, otherwise output_config canvas (so generate and G3 Validate stay coupled).
func (t Template) GenerationWidthHeight() (width, height int, seed int64) {
	w, h := t.ModelParameters.Width, t.ModelParameters.Height
	if w == 0 && h == 0 {
		return t.OutputConfig.CanvasWidth, t.OutputConfig.CanvasHeight, t.ModelParameters.Seed
	}
	return w, h, t.ModelParameters.Seed
}
