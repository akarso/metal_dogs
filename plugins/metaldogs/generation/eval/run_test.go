package eval_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/generation/eval"
)

func TestParseArgs(t *testing.T) {
	t.Parallel()
	opts, err := eval.ParseArgs([]string{
		"-template", "t.json",
		"-photos", "photos",
		"-out", "out",
		"-provider", "fake",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.TemplatePath != "t.json" || opts.PhotosDir != "photos" || opts.OutDir != "out" || opts.ProviderName != "fake" {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestRun_FakeProvider_WritesReportAndPNG(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tplPath := filepath.Join(root, "template.json")
	photosDir := filepath.Join(root, "photos")
	outDir := filepath.Join(root, "out")
	if err := os.MkdirAll(photosDir, 0o750); err != nil {
		t.Fatal(err)
	}
	tpl := `{
  "id": "eval-demo",
  "prompt": "metal dog on black tee",
  "prompt_version": "g0-v1",
  "dog_name": "Burek",
  "parameters": {"width": 8, "height": 8, "seed": 1}
}`
	if err := os.WriteFile(tplPath, []byte(tpl), 0o640); err != nil {
		t.Fatal(err)
	}
	// Minimal JPEG SOI marker bytes — fake does not decode.
	if err := os.WriteFile(filepath.Join(photosDir, "dog.jpg"), []byte{0xff, 0xd8, 0xff, 0xd9}, 0o640); err != nil {
		t.Fatal(err)
	}

	fixed := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	report, err := eval.Run(context.Background(), eval.Options{
		TemplatePath: tplPath,
		PhotosDir:    photosDir,
		OutDir:       outDir,
		ProviderName: "fake",
		Now:          func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.TemplateID != "eval-demo" || report.Provider != "fake" || len(report.Samples) != 1 {
		t.Fatalf("report = %+v", report)
	}
	sample := report.Samples[0]
	if sample.Error != "" || sample.OutputFile != "dog.png" || sample.Validation.Status != "skipped" {
		t.Fatalf("sample = %+v", sample)
	}
	if _, err := os.Stat(filepath.Join(outDir, "dog.png")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded eval.Report
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.GeneratedAt.Equal(fixed) || decoded.Samples[0].ProviderReqID == "" {
		t.Fatalf("decoded report = %+v", decoded)
	}
}

func TestLoadTemplate_YAML(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "t.yaml")
	body := "id: y\nprompt: hello\nprompt_version: v1\n"
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	def, err := eval.LoadTemplate(path)
	if err != nil {
		t.Fatal(err)
	}
	if def.ID != "y" || def.Prompt != "hello" || def.PromptVersion != "v1" {
		t.Fatalf("def = %+v", def)
	}
}

func TestAssembleRequestFields_FromTemplate(t *testing.T) {
	t.Parallel()
	// Request assembly is exercised end-to-end in TestRun; this guards LoadTemplate defaults.
	path := filepath.Join(t.TempDir(), "t.json")
	if err := os.WriteFile(path, []byte(`{"id":"a","prompt":"p"}`), 0o640); err != nil {
		t.Fatal(err)
	}
	def, err := eval.LoadTemplate(path)
	if err != nil {
		t.Fatal(err)
	}
	if def.PromptVersion != "g0-unversioned" || def.Parameters.Width != 64 {
		t.Fatalf("defaults = %+v", def)
	}
}
