package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/generation"
)

// Options configures an offline evaluation run.
type Options struct {
	TemplatePath string
	PhotosDir    string
	OutDir       string
	ProviderName string
	Now          func() time.Time // optional; defaults to time.Now.UTC
}

// Run loads the template, generates one output per photo, and writes a report.
func Run(ctx context.Context, opts Options) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.TemplatePath == "" {
		return Report{}, fmt.Errorf("eval: --template is required")
	}
	if opts.PhotosDir == "" {
		return Report{}, fmt.Errorf("eval: --photos is required")
	}
	if opts.OutDir == "" {
		return Report{}, fmt.Errorf("eval: --out is required")
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	def, err := LoadTemplate(opts.TemplatePath)
	if err != nil {
		return Report{}, err
	}
	provider, err := generation.Lookup(opts.ProviderName)
	if err != nil {
		return Report{}, err
	}
	photos, err := listPhotos(opts.PhotosDir)
	if err != nil {
		return Report{}, err
	}
	if len(photos) == 0 {
		return Report{}, fmt.Errorf("eval: no photos in %s (want .jpg/.jpeg/.png)", opts.PhotosDir)
	}
	if err := os.MkdirAll(opts.OutDir, 0o750); err != nil {
		return Report{}, fmt.Errorf("eval: mkdir out: %w", err)
	}

	report := Report{
		GeneratedAt:   now(),
		TemplateID:    def.ID,
		PromptVersion: def.PromptVersion,
		Provider:      provider.Name(),
		DogName:       def.DogName,
		OutDir:        opts.OutDir,
		Samples:       make([]SampleResult, 0, len(photos)),
	}

	for _, photoPath := range photos {
		sample := runOne(ctx, provider, def, photoPath, opts.OutDir)
		report.Samples = append(report.Samples, sample)
	}

	if _, err := WriteReport(opts.OutDir, report); err != nil {
		return Report{}, err
	}
	return report, nil
}

func runOne(ctx context.Context, provider generation.ImageProvider, def TemplateDef, photoPath, outDir string) SampleResult {
	base := filepath.Base(photoPath)
	sample := SampleResult{
		PhotoName: base,
		Validation: ValidationResult{
			Status:  "skipped",
			Message: "G3 output validation not wired yet",
		},
	}
	raw, err := os.ReadFile(photoPath)
	if err != nil {
		sample.Error = err.Error()
		sample.ErrorClass = string(generation.ErrorPermanent)
		return sample
	}

	req := generation.GenerationRequest{
		PhotoJPEG:     raw, // bytes as provided; fake does not decode
		PhotoName:     base,
		Prompt:        def.Prompt,
		PromptVersion: def.PromptVersion,
		DogName:       def.DogName,
		Provider:      provider.Name(),
		Model:         def.Model,
		Parameters:    def.Parameters,
	}
	res, err := provider.Generate(ctx, req)
	if err != nil {
		sample.Error = err.Error()
		if pe, ok := err.(*generation.ProviderError); ok {
			sample.ErrorClass = string(pe.Class)
		} else {
			sample.ErrorClass = string(generation.ErrorPermanent)
		}
		return sample
	}

	outName := strings.TrimSuffix(base, filepath.Ext(base)) + ".png"
	outPath := filepath.Join(outDir, outName)
	if err := os.WriteFile(outPath, res.ImagePNG, 0o640); err != nil {
		sample.Error = err.Error()
		sample.ErrorClass = string(generation.ErrorPermanent)
		return sample
	}
	sample.OutputFile = outName
	sample.ProviderReqID = res.ProviderReqID
	sample.Model = res.Model
	sample.LatencyMS = res.Latency.Milliseconds()
	sample.CostMinorUnits = res.CostMinorUnits
	sample.HasAlpha = res.HasAlpha
	return sample
}

func listPhotos(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("eval: read photos dir: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		switch strings.ToLower(filepath.Ext(name)) {
		case ".jpg", ".jpeg", ".png":
			out = append(out, filepath.Join(dir, name))
		}
	}
	sort.Strings(out)
	return out, nil
}
