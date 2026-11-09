package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"gopkg.in/yaml.v3"
)

// TemplateDef is the eval-facing view of a template (v1 or legacy stub).
type TemplateDef struct {
	ID            string                `json:"id" yaml:"id"`
	Prompt        string                `json:"prompt" yaml:"prompt"`
	PromptVersion string                `json:"prompt_version" yaml:"prompt_version"`
	DogName       string                `json:"dog_name" yaml:"dog_name"`
	Model         string                `json:"model" yaml:"model"`
	Parameters    generation.Parameters `json:"parameters" yaml:"parameters"`
	OutputConfig  domain.OutputConfig   `json:"output_config" yaml:"output_config"`
}

// LoadTemplate reads a Template v1 YAML/JSON or a legacy G0 stub.
// DogName is not taken from v1 files — set via Options.DogName / -dog-name.
func LoadTemplate(path string) (TemplateDef, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return TemplateDef{}, fmt.Errorf("eval: read template: %w", err)
	}
	if isV1Template(raw, path) {
		t, err := application.ValidatePath(path)
		if err != nil {
			return TemplateDef{}, fmt.Errorf("eval: %w", err)
		}
		return templateDefFromDomain(t), nil
	}
	return loadLegacyStub(raw, path)
}

func isV1Template(raw []byte, path string) bool {
	var probe struct {
		Slug               string `json:"slug" yaml:"slug"`
		PromptInstructions any    `json:"prompt_instructions" yaml:"prompt_instructions"`
		ID                 string `json:"id" yaml:"id"`
		Prompt             string `json:"prompt" yaml:"prompt"`
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		_ = yaml.Unmarshal(raw, &probe)
	default:
		_ = json.Unmarshal(raw, &probe)
	}
	return probe.Slug != "" && probe.PromptInstructions != nil
}

func templateDefFromDomain(t domain.Template) TemplateDef {
	w, h, seed := t.GenerationWidthHeight()
	return TemplateDef{
		ID:            t.Slug,
		Prompt:        t.PromptInstructions.Text,
		PromptVersion: t.PromptInstructions.PromptVersion,
		Model:         t.ModelParameters.Model,
		Parameters:    generation.Parameters{Width: w, Height: h, Seed: seed},
		OutputConfig:  t.OutputConfig,
	}
}

// legacyOutputProbe distinguishes omitted vs zero canvas / monochrome keys.
type legacyOutputProbe struct {
	CanvasWidth  *int              `json:"canvas_width" yaml:"canvas_width"`
	CanvasHeight *int              `json:"canvas_height" yaml:"canvas_height"`
	Monochrome   *bool             `json:"monochrome" yaml:"monochrome"`
	PrintArea    *domain.PrintArea `json:"print_area" yaml:"print_area"`
}

func loadLegacyStub(raw []byte, path string) (TemplateDef, error) {
	var def TemplateDef
	var ocWrap struct {
		OutputConfig *legacyOutputProbe `json:"output_config" yaml:"output_config"`
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(raw, &def); err != nil {
			return TemplateDef{}, fmt.Errorf("eval: parse yaml template: %w", err)
		}
		_ = yaml.Unmarshal(raw, &ocWrap)
	default:
		if err := json.Unmarshal(raw, &def); err != nil {
			return TemplateDef{}, fmt.Errorf("eval: parse json template: %w", err)
		}
		_ = json.Unmarshal(raw, &ocWrap)
	}
	if def.ID == "" {
		return TemplateDef{}, fmt.Errorf("eval: template id required")
	}
	if def.Prompt == "" {
		return TemplateDef{}, fmt.Errorf("eval: template prompt required")
	}
	if def.PromptVersion == "" {
		def.PromptVersion = "g0-unversioned"
	}
	w, h := def.Parameters.Width, def.Parameters.Height
	switch {
	case w == 0 && h == 0:
	case w > 0 && h > 0:
		if err := def.Parameters.Validate(); err != nil {
			return TemplateDef{}, fmt.Errorf("eval: template parameters: %w", err)
		}
	default:
		return TemplateDef{}, fmt.Errorf("eval: template parameters: width and height must both be set or both omitted (got %dx%d)", w, h)
	}
	if err := applyLegacyOutputDefaults(&def, ocWrap.OutputConfig); err != nil {
		return TemplateDef{}, err
	}
	return def, nil
}

// applyLegacyOutputDefaults requires both canvas edges or neither; defaults
// Monochrome to true when the key is omitted.
func applyLegacyOutputDefaults(def *TemplateDef, oc *legacyOutputProbe) error {
	if oc == nil {
		setDefaultCanvasMono(def)
		return nil
	}
	wPresent := oc.CanvasWidth != nil
	hPresent := oc.CanvasHeight != nil
	if wPresent != hPresent {
		return fmt.Errorf("eval: output_config canvas_width and canvas_height must both be set or both omitted")
	}
	if !wPresent {
		setDefaultCanvasMono(def)
		if oc.Monochrome != nil {
			def.OutputConfig.Monochrome = *oc.Monochrome
		}
		if oc.PrintArea != nil {
			def.OutputConfig.PrintArea = *oc.PrintArea
		}
		return nil
	}
	cw, ch := *oc.CanvasWidth, *oc.CanvasHeight
	if cw <= 0 || ch <= 0 {
		return fmt.Errorf("eval: output_config canvas_width and canvas_height must be > 0 (got %dx%d)", cw, ch)
	}
	if cw > domain.MaxCanvasDimension || ch > domain.MaxCanvasDimension {
		return fmt.Errorf("eval: output_config canvas exceeds max %d", domain.MaxCanvasDimension)
	}
	def.OutputConfig.CanvasWidth = cw
	def.OutputConfig.CanvasHeight = ch
	if oc.Monochrome != nil {
		def.OutputConfig.Monochrome = *oc.Monochrome
	} else {
		def.OutputConfig.Monochrome = true
	}
	if oc.PrintArea != nil {
		def.OutputConfig.PrintArea = *oc.PrintArea
	}
	return nil
}

func setDefaultCanvasMono(def *TemplateDef) {
	w, h := def.Parameters.Width, def.Parameters.Height
	if w == 0 {
		w = generation.DefaultDimension
	}
	if h == 0 {
		h = generation.DefaultDimension
	}
	def.OutputConfig.CanvasWidth = w
	def.OutputConfig.CanvasHeight = h
	def.OutputConfig.Monochrome = true
}
