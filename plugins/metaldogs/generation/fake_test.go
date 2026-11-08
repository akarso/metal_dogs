package generation_test

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/generation"
)

func TestFake_Generate_Deterministic(t *testing.T) {
	t.Parallel()
	p := generation.Fake()
	req := generation.GenerationRequest{
		PhotoBytes:    []byte{0xff, 0xd8, 0xff},
		PhotoName:     "dog.jpg",
		Prompt:        "metal dog",
		PromptVersion: "g0-v1",
		DogName:       "Burek",
		Parameters:    generation.Parameters{Width: 16, Height: 16, Seed: 7},
	}
	a, err := p.Generate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Generate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.ImagePNG, b.ImagePNG) {
		t.Fatal("expected identical PNG for identical request")
	}
	if a.ProviderReqID != b.ProviderReqID || a.ProviderReqID == "" {
		t.Fatalf("ProviderReqID = %q / %q", a.ProviderReqID, b.ProviderReqID)
	}
	img, err := png.Decode(bytes.NewReader(a.ImagePNG))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds(); got.Dx() != 16 || got.Dy() != 16 {
		t.Fatalf("bounds = %v, want 16x16", got)
	}
}

func TestFake_ProviderReqID_DistinguishesSeeds(t *testing.T) {
	t.Parallel()
	p := generation.Fake()
	base := generation.GenerationRequest{
		PhotoBytes:    []byte{1, 2, 3},
		PhotoName:     "dog.jpg",
		Prompt:        "p",
		PromptVersion: "v1",
		Parameters:    generation.Parameters{Width: 8, Height: 8, Seed: 1},
	}
	a, err := p.Generate(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	base.Parameters.Seed = 257 // same as 1 mod 256 — must not collide
	b, err := p.Generate(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if a.ProviderReqID == b.ProviderReqID {
		t.Fatalf("seeds 1 and 257 collided: %s", a.ProviderReqID)
	}
	if bytes.Equal(a.ImagePNG, b.ImagePNG) {
		t.Fatal("PNG pixels collided for seeds 1 and 257")
	}
}

func TestFake_Generate_RejectsEmptyPhotoBytes(t *testing.T) {
	t.Parallel()
	_, err := generation.Fake().Generate(context.Background(), generation.GenerationRequest{
		PhotoName: "x.jpg",
		Prompt:    "p",
	})
	pe, ok := err.(*generation.ProviderError)
	if !ok || pe.Class != generation.ErrorRejectedInput {
		t.Fatalf("err = %v, want rejected_input ProviderError", err)
	}
}

func TestFake_Generate_RejectsEmptyPrompt(t *testing.T) {
	t.Parallel()
	_, err := generation.Fake().Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes: []byte{1},
		PhotoName:  "x.jpg",
	})
	pe, ok := err.(*generation.ProviderError)
	if !ok || pe.Class != generation.ErrorRejectedInput {
		t.Fatalf("err = %v, want rejected_input ProviderError", err)
	}
}

func TestFake_Generate_RejectsOversize(t *testing.T) {
	t.Parallel()
	_, err := generation.Fake().Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes: []byte{1},
		Prompt:     "p",
		Parameters: generation.Parameters{Width: generation.MaxDimension + 1, Height: 64},
	})
	if generation.ClassifyError(err) != generation.ErrorRejectedInput {
		t.Fatalf("err = %v, want rejected_input", err)
	}
}

func TestFake_Generate_RejectsHugePhoto(t *testing.T) {
	t.Parallel()
	huge := make([]byte, generation.MaxPhotoBytes+1)
	_, err := generation.Fake().Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes: huge,
		Prompt:     "p",
		Parameters: generation.Parameters{Width: 4, Height: 4},
	})
	if generation.ClassifyError(err) != generation.ErrorRejectedInput {
		t.Fatalf("err = %v, want rejected_input", err)
	}
}

func TestFake_Generate_ContextCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := generation.Fake().Generate(ctx, generation.GenerationRequest{
		PhotoBytes: []byte{1},
		Prompt:     "p",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if generation.ClassifyError(err) != generation.ErrorCanceled {
		t.Fatalf("class = %s", generation.ClassifyError(err))
	}
}

func TestLookup_FakeDefault(t *testing.T) {
	t.Parallel()
	p, err := generation.Lookup("")
	if err != nil || p.Name() != "fake" {
		t.Fatalf("Lookup(\"\") = (%v, %v)", p, err)
	}
	if _, err := generation.Lookup("openai"); err == nil {
		t.Fatal("expected unknown provider error")
	}
}

func TestClassifyError_WrappedProviderError(t *testing.T) {
	t.Parallel()
	inner := &generation.ProviderError{Class: generation.ErrorRateLimited, Message: "slow"}
	wrapped := errors.Join(errors.New("adapter"), inner)
	if generation.ClassifyError(wrapped) != generation.ErrorRateLimited {
		t.Fatalf("class = %s", generation.ClassifyError(wrapped))
	}
}
