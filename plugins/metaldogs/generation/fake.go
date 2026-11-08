package generation

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"time"
)

const fakeModel = "fake-g0-v1"

type fakeProvider struct{}

// Fake returns the deterministic fixture ImageProvider (default everywhere).
func Fake() ImageProvider { return fakeProvider{} }

func (fakeProvider) Name() string { return "fake" }

func (fakeProvider) Generate(ctx context.Context, req GenerationRequest) (GenerationResult, error) {
	if err := ctx.Err(); err != nil {
		return GenerationResult{}, err
	}
	if len(req.PhotoBytes) == 0 {
		return GenerationResult{}, &ProviderError{Class: ErrorRejectedInput, Message: "photo bytes required"}
	}
	if len(req.PhotoBytes) > MaxPhotoBytes {
		return GenerationResult{}, &ProviderError{
			Class:   ErrorRejectedInput,
			Message: fmt.Sprintf("photo exceeds %d bytes", MaxPhotoBytes),
		}
	}
	if req.Prompt == "" {
		return GenerationResult{}, &ProviderError{Class: ErrorRejectedInput, Message: "prompt required"}
	}

	params := req.Parameters
	params.Normalize()
	if err := params.Validate(); err != nil {
		return GenerationResult{}, err
	}

	start := time.Now()
	img, hasAlpha, digest, err := renderFakePNG(req, params)
	if err != nil {
		return GenerationResult{}, &ProviderError{Class: ErrorPermanent, Message: err.Error()}
	}
	latency := time.Since(start)

	return GenerationResult{
		ImagePNG:       img,
		HasAlpha:       hasAlpha,
		Latency:        latency,
		CostMinorUnits: 0,
		ProviderReqID:  fmt.Sprintf("fake-%x", digest),
		Model:          fakeModel,
	}, nil
}

func renderFakePNG(req GenerationRequest, params Parameters) ([]byte, bool, []byte, error) {
	digest := requestDigest(req, params)

	r, g, b := digest[0], digest[1], digest[2]
	a := digest[3]
	if a < 200 {
		a = 255 // print-ish fixtures stay mostly opaque
	}
	hasAlpha := a < 255

	img := image.NewRGBA(image.Rect(0, 0, params.Width, params.Height))
	c := color.RGBA{R: r, G: g, B: b, A: a}
	for y := 0; y < params.Height; y++ {
		for x := 0; x < params.Width; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	// Deterministic 1px border from digest so golden checks can assert structure.
	border := color.RGBA{R: digest[4], G: digest[5], B: digest[6], A: 255}
	for x := 0; x < params.Width; x++ {
		img.SetRGBA(x, 0, border)
		img.SetRGBA(x, params.Height-1, border)
	}
	for y := 0; y < params.Height; y++ {
		img.SetRGBA(0, y, border)
		img.SetRGBA(params.Width-1, y, border)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, false, nil, err
	}
	return buf.Bytes(), hasAlpha, digest, nil
}
