package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/generation"
)

// ValidationResult is a placeholder slot for G3 checks (skipped in G0).
type ValidationResult struct {
	Status  string   `json:"status"` // skipped | pass | fail
	Codes   []string `json:"codes,omitempty"`
	Message string   `json:"message,omitempty"`
}

// SampleResult is one photo's generation outcome in the report.
type SampleResult struct {
	PhotoName      string           `json:"photo_name"`
	OutputFile     string           `json:"output_file,omitempty"`
	ProviderReqID  string           `json:"provider_request_id,omitempty"`
	Model          string           `json:"model,omitempty"`
	LatencyMS      int64            `json:"latency_ms"`
	CostMinorUnits int64            `json:"cost_minor_units"`
	HasAlpha       bool             `json:"has_alpha"`
	Error          string           `json:"error,omitempty"`
	ErrorClass     string           `json:"error_class,omitempty"`
	Validation     ValidationResult `json:"validation"`
}

// Report is the JSON summary written next to output images.
type Report struct {
	GeneratedAt   time.Time             `json:"generated_at"`
	TemplateID    string                `json:"template_id"`
	PromptVersion string                `json:"prompt_version"`
	Provider      string                `json:"provider"`
	Model         string                `json:"model,omitempty"`
	DogName       string                `json:"dog_name,omitempty"`
	Parameters    generation.Parameters `json:"parameters"`
	OutDir        string                `json:"out_dir"`
	Samples       []SampleResult        `json:"samples"`
}

// WriteReport encodes report as indented JSON at outDir/report.json.
func WriteReport(outDir string, report Report) (string, error) {
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return "", fmt.Errorf("eval: mkdir out: %w", err)
	}
	path := filepath.Join(outDir, "report.json")
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("eval: marshal report: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		return "", fmt.Errorf("eval: write report: %w", err)
	}
	return path, nil
}
