package openai_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"github.com/akarso/shopanda/plugins/metaldogs/generation/openai"
)

// tinyPNG is a 1x1 opaque PNG (sanitized fixture bytes, not a customer photo).
var tinyPNG = mustDecode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

func mustDecode(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func TestProvider_Generate_ContractHappy(t *testing.T) {
	t.Parallel()
	outB64 := base64.StdEncoding.EncodeToString(tinyPNG)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer test-key") {
			t.Fatalf("auth = %q", r.Header.Get("Authorization"))
		}
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("prompt") == "" || r.FormValue("model") == "" {
			t.Fatalf("form = %#v", r.Form)
		}
		if _, _, err := r.FormFile("image"); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1700000000,"data":[{"b64_json":"`+outB64+`"}]}`)
	}))
	defer srv.Close()

	p, err := openai.New(openai.Config{
		APIKey:     "test-key",
		BaseURL:    srv.URL + "/v1",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes:    tinyPNG,
		PhotoName:     "dog.png",
		Prompt:        "metal dog portrait",
		PromptVersion: "g1-v1",
		DogName:       "Burek",
		Parameters:    generation.Parameters{Width: 64, Height: 64},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != openai.DefaultModel || res.ProviderReqID != "openai-1700000000" {
		t.Fatalf("res = %+v", res)
	}
	if string(res.ImagePNG) != string(tinyPNG) {
		t.Fatal("png mismatch")
	}
}

func TestProvider_Generate_RateLimited(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"slow down","type":"rate_limit"}}`)
	}))
	defer srv.Close()
	p, err := openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes: tinyPNG,
		Prompt:     "p",
	})
	if generation.ClassifyError(err) != generation.ErrorRateLimited {
		t.Fatalf("err = %v", err)
	}
}

func TestProvider_Generate_RejectsMissingKey(t *testing.T) {
	t.Parallel()
	if _, err := openai.New(openai.Config{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestProvider_Generate_ContextCanceled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	p, err := openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Generate(ctx, generation.GenerationRequest{PhotoBytes: tinyPNG, Prompt: "p"})
	if generation.ClassifyError(err) != generation.ErrorCanceled {
		t.Fatalf("err = %v class=%s", err, generation.ClassifyError(err))
	}
}
