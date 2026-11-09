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

func loadLegacyStub(raw []byte, path string) (TemplateDef, error) {
	var def TemplateDef
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(raw, &def); err != nil {
			return TemplateDef{}, fmt.Errorf("eval: parse yaml template: %w", err)
		}
	default:
		if err := json.Unmarshal(raw, &def); err != nil {
			return TemplateDef{}, fmt.Errorf("eval: parse json template: %w", err)
		}
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
	applyLegacyOutputDefaults(&def)
	return def, nil
}

// applyLegacyOutputDefaults fills OutputConfig from Parameters (or 64×64) with
// Monochrome true when the stub omitted canvas size.
func applyLegacyOutputDefaults(def *TemplateDef) {
	if def.OutputConfig.CanvasWidth > 0 || def.OutputConfig.CanvasHeight > 0 {
		return
	}
	w, h := def.Parameters.Width, def.Parameters.Height
	if w == 0 {
		w = generation.DefaultDimension
	}
	if h == 0 {
		h = generation.DefaultDimension
	}
	def.OutputConfig = domain.OutputConfig{
		CanvasWidth:  w,
		CanvasHeight: h,
		Monochrome:   true,
	}
}
