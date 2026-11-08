package openai_test

import (
	"context"
	"os"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"github.com/akarso/shopanda/plugins/metaldogs/generation/openai"
)

// Opt-in paid smoke: never runs in default CI.
//   METALDOGS_SMOKE=1 METALDOGS_OPENAI_API_KEY=sk-... go test ./plugins/metaldogs/generation/openai -run Smoke -count=1
func TestSmoke_OpenAI_Edits(t *testing.T) {
	if os.Getenv("METALDOGS_SMOKE") != "1" {
		t.Skip("set METALDOGS_SMOKE=1 to run paid OpenAI smoke")
	}
	key := os.Getenv("METALDOGS_OPENAI_API_KEY")
	if key == "" {
		t.Fatal("METALDOGS_OPENAI_API_KEY required when METALDOGS_SMOKE=1")
	}
	p, err := openai.New(openai.Config{APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes:    tinyPNG,
		PhotoName:     "fixture.png",
		Prompt:        "Monochrome metal-band style dog portrait on transparent-friendly background. High contrast black and white.",
		PromptVersion: "g1-smoke",
		DogName:       "Burek",
		Parameters:    generation.Parameters{Width: 1024, Height: 1024},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ImagePNG) < 100 || res.ProviderReqID == "" {
		t.Fatalf("unexpected result: bytes=%d id=%q", len(res.ImagePNG), res.ProviderReqID)
	}
	t.Logf("smoke ok model=%s id=%s latency=%s bytes=%d", res.Model, res.ProviderReqID, res.Latency, len(res.ImagePNG))
}
