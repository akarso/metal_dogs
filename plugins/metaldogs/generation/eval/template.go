package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"gopkg.in/yaml.v3"
)

// TemplateDef is the G0 file-backed template stub (full format lands in G2).
type TemplateDef struct {
	ID            string                `json:"id" yaml:"id"`
	Prompt        string                `json:"prompt" yaml:"prompt"`
	PromptVersion string                `json:"prompt_version" yaml:"prompt_version"`
	DogName       string                `json:"dog_name" yaml:"dog_name"`
	Model         string                `json:"model" yaml:"model"`
	Parameters    generation.Parameters `json:"parameters" yaml:"parameters"`
}

// LoadTemplate reads a JSON or YAML template definition from path.
func LoadTemplate(path string) (TemplateDef, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return TemplateDef{}, fmt.Errorf("eval: read template: %w", err)
	}
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
	if def.Model == "" {
		def.Model = "unset"
	}
	def.Parameters.Normalize()
	if err := def.Parameters.Validate(); err != nil {
		return TemplateDef{}, fmt.Errorf("eval: template parameters: %w", err)
	}
	return def, nil
}
