package eval_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/generation"
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

func TestParseArgs_MissingRequired(t *testing.T) {
	t.Parallel()
	opts, err := eval.ParseArgs([]string{"-provider", "fake"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eval.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error when template/photos/out missing")
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
  "model": "tpl-model",
  "parameters": {"width": 8, "height": 8, "seed": 1}
}`
	if err := os.WriteFile(tplPath, []byte(tpl), 0o640); err != nil {
		t.Fatal(err)
	}
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
	if report.TemplateID != "eval-demo" || report.Provider != "fake" || report.TemplateModel != "tpl-model" {
		t.Fatalf("report = %+v", report)
	}
	if report.Parameters.Seed != 1 || report.Parameters.Width != 8 {
		t.Fatalf("parameters = %+v", report.Parameters)
	}
	sample := report.Samples[0]
	if sample.Error != "" || sample.OutputFile != "dog.jpg.png" || sample.Validation.Status != "skipped" {
		t.Fatalf("sample = %+v", sample)
	}
	if sample.ProviderModel != "fake-g0-v1" {
		t.Fatalf("provider_model = %q", sample.ProviderModel)
	}
	if _, err := os.Stat(filepath.Join(outDir, "dog.jpg.png")); err != nil {
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
	if !decoded.GeneratedAt.Equal(fixed) || decoded.Samples[0].ProviderReqID == "" || decoded.Parameters.Seed != 1 {
		t.Fatalf("decoded report = %+v", decoded)
	}
}

func TestRun_DistinctOutputNames_ForJPEGVariants(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tplPath := filepath.Join(root, "template.json")
	photosDir := filepath.Join(root, "photos")
	outDir := filepath.Join(root, "out")
	if err := os.MkdirAll(photosDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tplPath, []byte(`{"id":"c","prompt":"p","parameters":{"width":4,"height":4,"seed":1}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dog.jpg", "dog.jpeg", "dog.JPG"} {
		if err := os.WriteFile(filepath.Join(photosDir, name), []byte{0xff, 0xd8, 0xff, byte(len(name))}, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	report, err := eval.Run(context.Background(), eval.Options{
		TemplatePath: tplPath,
		PhotosDir:    photosDir,
		OutDir:       outDir,
		ProviderName: "fake",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Samples) != 3 {
		t.Fatalf("samples = %d", len(report.Samples))
	}
	seen := map[string]bool{}
	for _, s := range report.Samples {
		if s.Error != "" {
			t.Fatalf("sample error: %+v", s)
		}
		if seen[s.OutputFile] {
			t.Fatalf("duplicate output_file %q", s.OutputFile)
		}
		seen[s.OutputFile] = true
		if _, err := os.Stat(filepath.Join(outDir, s.OutputFile)); err != nil {
			t.Fatal(err)
		}
	}
	if !seen["dog.jpg.png"] || !seen["dog.jpeg.png"] || !seen["dog.JPG.png"] {
		t.Fatalf("seen = %#v", seen)
	}
}

func TestRun_RejectsNonEmptyOut(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tplPath := filepath.Join(root, "t.json")
	photosDir := filepath.Join(root, "photos")
	outDir := filepath.Join(root, "out")
	if err := os.MkdirAll(photosDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tplPath, []byte(`{"id":"a","prompt":"p"}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(photosDir, "a.jpg"), []byte{1}, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "stale.png"), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	_, err := eval.Run(context.Background(), eval.Options{
		TemplatePath: tplPath,
		PhotosDir:    photosDir,
		OutDir:       outDir,
	})
	if err == nil {
		t.Fatal("expected non-empty out error")
	}
}

func TestRun_AllowsDotfilesInOut(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tplPath := filepath.Join(root, "t.json")
	photosDir := filepath.Join(root, "photos")
	outDir := filepath.Join(root, "out")
	if err := os.MkdirAll(photosDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tplPath, []byte(`{"id":"a","prompt":"p","parameters":{"width":4,"height":4}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(photosDir, "a.jpg"), []byte{1}, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, ".gitkeep"), nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := eval.Run(context.Background(), eval.Options{
		TemplatePath: tplPath,
		PhotosDir:    photosDir,
		OutDir:       outDir,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRun_EmptyPhotoBytes_MarkedFailed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tplPath := filepath.Join(root, "t.json")
	photosDir := filepath.Join(root, "photos")
	outDir := filepath.Join(root, "out")
	if err := os.MkdirAll(photosDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tplPath, []byte(`{"id":"a","prompt":"p","parameters":{"width":4,"height":4}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(photosDir, "empty.jpg"), nil, 0o640); err != nil {
		t.Fatal(err)
	}
	report, err := eval.Run(context.Background(), eval.Options{
		TemplatePath: tplPath,
		PhotosDir:    photosDir,
		OutDir:       outDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Samples) != 1 || report.Samples[0].ErrorClass != string(generation.ErrorRejectedInput) {
		t.Fatalf("sample = %+v", report.Samples[0])
	}
}

func TestRun_ContextCancel_Classified(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tplPath := filepath.Join(root, "t.json")
	photosDir := filepath.Join(root, "photos")
	outDir := filepath.Join(root, "out")
	if err := os.MkdirAll(photosDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tplPath, []byte(`{"id":"a","prompt":"p","parameters":{"width":4,"height":4}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(photosDir, "a.jpg"), []byte{1}, 0o640); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := eval.Run(ctx, eval.Options{
		TemplatePath: tplPath,
		PhotosDir:    photosDir,
		OutDir:       outDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Samples[0].ErrorClass != string(generation.ErrorCanceled) {
		t.Fatalf("sample = %+v", report.Samples[0])
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

func TestLoadTemplate_RejectsOversize(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "t.json")
	body := `{"id":"a","prompt":"p","parameters":{"width":99999,"height":64}}`
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := eval.LoadTemplate(path); err == nil {
		t.Fatal("expected oversize error")
	}
}

func TestAssembleRequestFields_FromTemplate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "t.json")
	if err := os.WriteFile(path, []byte(`{"id":"a","prompt":"p"}`), 0o640); err != nil {
		t.Fatal(err)
	}
	def, err := eval.LoadTemplate(path)
	if err != nil {
		t.Fatal(err)
	}
	if def.PromptVersion != "g0-unversioned" || def.Model != "" || def.Parameters.Width != 0 || def.Parameters.Height != 0 {
		t.Fatalf("defaults = %+v", def)
	}
}

func TestLoadTemplate_RejectsPartialCanvas(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "t.json")
	body := `{"id":"a","prompt":"p","parameters":{"width":1024}}`
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := eval.LoadTemplate(path); err == nil {
		t.Fatal("expected partial width/height error")
	}
}

func TestLoadTemplate_V1YAML(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "templates", "head-front-blackmetal.yaml")
	def, err := eval.LoadTemplate(path)
	if err != nil {
		t.Fatal(err)
	}
	if def.ID != "head-front-blackmetal" || def.PromptVersion != "g2-v1" || def.Model == "" {
		t.Fatalf("def = %+v", def)
	}
	if def.Parameters.Width != 1024 || def.DogName != "" {
		t.Fatalf("v1 must not embed dog_name; params=%+v dog=%q", def.Parameters, def.DogName)
	}
}

func TestRun_V1Template_FakeProvider(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	photosDir := filepath.Join(root, "photos")
	outDir := filepath.Join(root, "out")
	if err := os.MkdirAll(photosDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(photosDir, "dog.png"), tinyPNG(t), 0o640); err != nil {
		t.Fatal(err)
	}
	report, err := eval.Run(context.Background(), eval.Options{
		TemplatePath: filepath.Join("..", "..", "templates", "short-name-punk.yaml"),
		PhotosDir:    photosDir,
		OutDir:       outDir,
		ProviderName: "fake",
		DogName:      "Rex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.TemplateID != "short-name-punk" || report.DogName != "Rex" {
		t.Fatalf("report = %+v", report)
	}
	if report.Samples[0].Error != "" {
		t.Fatalf("sample: %+v", report.Samples[0])
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	// 1x1 opaque PNG
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe, 0xd4, 0xef, 0x00, 0x00,
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
}
