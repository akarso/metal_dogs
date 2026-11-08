package generation_test

import (
	"bytes"
	"context"
	"image/png"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/generation"
)

func TestFake_Generate_Deterministic(t *testing.T) {
	t.Parallel()
	p := generation.Fake()
	req := generation.GenerationRequest{
		PhotoJPEG:     []byte{0xff, 0xd8, 0xff},
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
	if !a.HasAlpha {
		t.Fatal("HasAlpha = false, want true")
	}
	img, err := png.Decode(bytes.NewReader(a.ImagePNG))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds(); got.Dx() != 16 || got.Dy() != 16 {
		t.Fatalf("bounds = %v, want 16x16", got)
	}
}

func TestFake_Generate_RejectsEmptyPrompt(t *testing.T) {
	t.Parallel()
	_, err := generation.Fake().Generate(context.Background(), generation.GenerationRequest{
		PhotoName: "x.jpg",
	})
	pe, ok := err.(*generation.ProviderError)
	if !ok || pe.Class != generation.ErrorRejectedInput {
		t.Fatalf("err = %v, want rejected_input ProviderError", err)
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
