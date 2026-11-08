package generation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
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
	if len(req.PhotoJPEG) == 0 && len(req.PhotoName) == 0 {
		return GenerationResult{}, &ProviderError{Class: ErrorRejectedInput, Message: "photo required"}
	}
	if req.Prompt == "" {
		return GenerationResult{}, &ProviderError{Class: ErrorRejectedInput, Message: "prompt required"}
	}

	params := req.Parameters
	params.Normalize()

	start := time.Now()
	img, err := renderFakePNG(req, params)
	if err != nil {
		return GenerationResult{}, &ProviderError{Class: ErrorPermanent, Message: err.Error()}
	}
	latency := time.Since(start)

	sum := sha256.Sum256(append(append([]byte(req.PromptVersion), req.PhotoName...), byte(params.Seed)))
	reqID := fmt.Sprintf("fake-%x", sum[:8])

	return GenerationResult{
		ImagePNG:       img,
		HasAlpha:       true,
		Latency:        latency,
		CostMinorUnits: 0,
		ProviderReqID:  reqID,
		Model:          fakeModel,
	}, nil
}

func renderFakePNG(req GenerationRequest, params Parameters) ([]byte, error) {
	h := sha256.New()
	_, _ = h.Write([]byte(req.Prompt))
	_, _ = h.Write([]byte(req.PromptVersion))
	_, _ = h.Write([]byte(req.DogName))
	_, _ = h.Write([]byte(req.PhotoName))
	var seedBuf [8]byte
	binary.LittleEndian.PutUint64(seedBuf[:], uint64(params.Seed))
	_, _ = h.Write(seedBuf[:])
	_, _ = h.Write(req.PhotoJPEG)
	digest := h.Sum(nil)

	r, g, b := digest[0], digest[1], digest[2]
	a := digest[3]
	if a < 200 {
		a = 255 // keep mostly opaque for print-ish fixtures; still marks HasAlpha
	}

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
		return nil, err
	}
	return buf.Bytes(), nil
}
