// Package generation defines the ImageProvider port and offline evaluation
// types for Metal Dogs (ADR-0008 / G0).
package generation

import (
	"context"
	"fmt"
	"time"
)

// ErrorClass classifies provider failures for retry / UX (ADR-0008).
type ErrorClass string

const (
	ErrorTransient     ErrorClass = "transient"
	ErrorPermanent     ErrorClass = "permanent"
	ErrorRejectedInput ErrorClass = "rejected_input"
	ErrorRateLimited   ErrorClass = "rate_limited"
)

// ProviderError is returned by adapters instead of leaking raw provider payloads.
type ProviderError struct {
	Class   ErrorClass
	Message string
}

func (e *ProviderError) Error() string {
	if e == nil {
		return "generation: nil provider error"
	}
	return fmt.Sprintf("generation: %s: %s", e.Class, e.Message)
}

// Parameters are typed generation knobs recorded on every job/report.
type Parameters struct {
	Width  int   `json:"width" yaml:"width"`
	Height int   `json:"height" yaml:"height"`
	Seed   int64 `json:"seed" yaml:"seed"`
}

// Normalize applies defaults used by the fake provider and eval harness.
func (p *Parameters) Normalize() {
	if p.Width <= 0 {
		p.Width = 64
	}
	if p.Height <= 0 {
		p.Height = 64
	}
}

// GenerationRequest is the normalized input to ImageProvider.Generate.
type GenerationRequest struct {
	PhotoJPEG      []byte
	PhotoName      string
	ReferencePNG   []byte
	Prompt         string
	PromptVersion  string
	DogName        string
	Provider       string
	Model          string
	Parameters     Parameters
}

// GenerationResult is the provider output plus support metadata.
type GenerationResult struct {
	ImagePNG       []byte
	HasAlpha       bool
	Latency        time.Duration
	CostMinorUnits int64  // 0 when provider does not report cost
	ProviderReqID  string
	Model          string
}

// ImageProvider is the generation port (ADR-0008). Exactly one real adapter
// plus fake may exist; switching is config (later slices).
type ImageProvider interface {
	Name() string
	Generate(ctx context.Context, req GenerationRequest) (GenerationResult, error)
}

// Lookup returns a known provider by name. Unknown names return an error.
func Lookup(name string) (ImageProvider, error) {
	switch name {
	case "", "fake":
		return Fake(), nil
	default:
		return nil, fmt.Errorf("generation: unknown provider %q (G0 supports fake only)", name)
	}
}
