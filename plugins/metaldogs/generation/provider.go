// Package generation defines the ImageProvider port and offline evaluation
// types for Metal Dogs (ADR-0008 / G0).
package generation

import (
	"context"
	"errors"
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
	ErrorCanceled      ErrorClass = "canceled"
)

// DefaultDimension is applied when width/height are unset.
const DefaultDimension = 64

// MaxDimension caps template/provider canvas size (OOM guard).
const MaxDimension = 2048

// MaxPhotoBytes caps inbound photo payload size before hashing/holding bytes.
const MaxPhotoBytes = 20 << 20 // 20 MiB

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
		p.Width = DefaultDimension
	}
	if p.Height <= 0 {
		p.Height = DefaultDimension
	}
}

// Validate rejects oversize canvases after Normalize.
func (p Parameters) Validate() error {
	if p.Width > MaxDimension || p.Height > MaxDimension {
		return &ProviderError{
			Class:   ErrorRejectedInput,
			Message: fmt.Sprintf("width/height must be <= %d (got %dx%d)", MaxDimension, p.Width, p.Height),
		}
	}
	if p.Width <= 0 || p.Height <= 0 {
		return &ProviderError{Class: ErrorRejectedInput, Message: "width/height must be positive"}
	}
	return nil
}

// GenerationRequest is the normalized input to ImageProvider.Generate.
type GenerationRequest struct {
	PhotoBytes    []byte // raw upload bytes (JPEG/PNG/…); adapters validate as needed
	PhotoName     string
	ReferencePNG  []byte
	Prompt        string
	PromptVersion string
	DogName       string
	Provider      string
	Model         string
	Parameters    Parameters
}

// GenerationResult is the provider output plus support metadata.
type GenerationResult struct {
	ImagePNG          []byte
	HasAlpha          bool
	Latency           time.Duration
	CostMinorUnits    int64 // 0 when provider does not report USD cents; prefer Usage* tokens
	UsageInputTokens  int
	UsageOutputTokens int
	UsageTotalTokens  int
	ProviderReqID     string
	Model             string
}

// ImageProvider is the generation port (ADR-0008). Exactly one real adapter
// plus fake may exist; switching is config (later slices).
type ImageProvider interface {
	Name() string
	Generate(ctx context.Context, req GenerationRequest) (GenerationResult, error)
}

// Lookup returns the fake provider (default). Real providers are constructed
// by their packages (e.g. generation/openai) and wired by eval/CLI to avoid
// import cycles.
func Lookup(name string) (ImageProvider, error) {
	switch name {
	case "", "fake":
		return Fake(), nil
	default:
		return nil, fmt.Errorf("generation: unknown provider %q (fake via Lookup; openai via generation/openai.New or eval -provider openai)", name)
	}
}

// ClassifyError maps provider/context errors to an ErrorClass for reports.
func ClassifyError(err error) ErrorClass {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrorCanceled
	}
	var pe *ProviderError
	if errors.As(err, &pe) && pe != nil {
		return pe.Class
	}
	return ErrorPermanent
}
