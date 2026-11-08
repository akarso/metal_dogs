package openai_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"github.com/akarso/shopanda/plugins/metaldogs/generation/openai"
)

func encodePNG(t *testing.T, w, h int, withAlpha bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	a := uint8(255)
	if withAlpha {
		a = 128
	}
	c := color.RGBA{R: 10, G: 20, B: 30, A: a}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProvider_Generate_ContractHappy(t *testing.T) {
	t.Parallel()
	outPNG := encodePNG(t, 8, 8, true)
	outB64 := base64.StdEncoding.EncodeToString(outPNG)
	var sawResponseFormat bool
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
		if r.FormValue("response_format") != "" {
			sawResponseFormat = true
		}
		if r.FormValue("model") != openai.DefaultModel {
			t.Fatalf("model = %q", r.FormValue("model"))
		}
		if r.FormValue("size") != "1024x1024" {
			t.Fatalf("size = %q (want live-legal size)", r.FormValue("size"))
		}
		if r.FormValue("background") != "transparent" || r.FormValue("output_format") != "png" {
			t.Fatalf("background/output_format = %q / %q", r.FormValue("background"), r.FormValue("output_format"))
		}
		if !strings.Contains(r.FormValue("prompt"), `Dog name to render exactly: "Burek"`) {
			t.Fatalf("prompt missing dog name: %q", r.FormValue("prompt"))
		}
		f, hdr, err := r.FormFile("image[]")
		if err != nil {
			t.Fatalf("image[]: %v", err)
		}
		defer f.Close()
		if ct := hdr.Header.Get("Content-Type"); ct != "image/png" {
			t.Fatalf("image Content-Type = %q", ct)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "req_abc123")
		_, _ = io.WriteString(w, `{"created":1700000000,"usage":{"input_tokens":11,"output_tokens":22,"total_tokens":33},"data":[{"b64_json":"`+outB64+`"}]}`)
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
	photo := encodePNG(t, 32, 32, false)
	res, err := p.Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes:    photo,
		PhotoName:     "dog.png",
		Prompt:        "metal dog portrait",
		PromptVersion: "g1-v1",
		DogName:       "Burek",
		Parameters:    generation.Parameters{Width: 1024, Height: 1024},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sawResponseFormat {
		t.Fatal("must not send response_format")
	}
	if res.Model != openai.DefaultModel || res.ProviderReqID != "req_abc123" {
		t.Fatalf("res = %+v", res)
	}
	if res.UsageTotalTokens != 33 || res.UsageInputTokens != 11 {
		t.Fatalf("usage = %+v", res)
	}
	if !res.HasAlpha {
		t.Fatal("HasAlpha = false, want true for transparent fixture")
	}
	if !bytes.Equal(res.ImagePNG, outPNG) {
		t.Fatal("png mismatch")
	}
}

func TestProvider_Generate_UnsetSizeUsesDefault(t *testing.T) {
	t.Parallel()
	outPNG := encodePNG(t, 4, 4, false)
	outB64 := base64.StdEncoding.EncodeToString(outPNG)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		if r.FormValue("size") != openai.DefaultSize {
			t.Fatalf("size = %q", r.FormValue("size"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1,"data":[{"b64_json":"`+outB64+`"}]}`)
	}))
	defer srv.Close()
	p, err := openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes: encodePNG(t, 8, 8, false),
		PhotoName:  "x.png",
		Prompt:     "p",
		// no Parameters — must not become fake DefaultDimension 64x64
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProvider_Generate_RejectsIllegalSize(t *testing.T) {
	t.Parallel()
	p, err := openai.New(openai.Config{APIKey: "k", BaseURL: "http://127.0.0.1:9"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []generation.Parameters{
		{Width: 64, Height: 64},
		{Width: 800, Height: 600},
		{Width: 1024, Height: 0},
	}
	for _, params := range cases {
		_, err := p.Generate(context.Background(), generation.GenerationRequest{
			PhotoBytes: encodePNG(t, 8, 8, false),
			PhotoName:  "x.png",
			Prompt:     "p",
			Parameters: params,
		})
		if generation.ClassifyError(err) != generation.ErrorRejectedInput {
			t.Fatalf("params=%+v err=%v", params, err)
		}
	}
}

func TestProvider_Generate_PrefersReqModel(t *testing.T) {
	t.Parallel()
	outPNG := encodePNG(t, 4, 4, false)
	outB64 := base64.StdEncoding.EncodeToString(outPNG)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		if r.FormValue("model") != "gpt-image-2.5-flare" {
			t.Fatalf("model = %q", r.FormValue("model"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1,"data":[{"b64_json":"`+outB64+`"}]}`)
	}))
	defer srv.Close()
	p, err := openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL, Model: openai.DefaultModel, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes: encodePNG(t, 8, 8, false),
		PhotoName:  "x.png",
		Prompt:     "p",
		Model:      "gpt-image-2.5-flare",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != "gpt-image-2.5-flare" {
		t.Fatalf("res.Model = %q", res.Model)
	}
}

func TestProvider_Generate_RejectsShutdownModelOnGenerate(t *testing.T) {
	t.Parallel()
	p, err := openai.New(openai.Config{APIKey: "k", BaseURL: "http://127.0.0.1:9"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes: encodePNG(t, 8, 8, false),
		PhotoName:  "x.png",
		Prompt:     "p",
		Model:      "gpt-image-1",
	})
	if generation.ClassifyError(err) != generation.ErrorRejectedInput {
		t.Fatalf("err = %v", err)
	}
}

func TestProvider_New_RejectsShutdownModel(t *testing.T) {
	t.Parallel()
	if _, err := openai.New(openai.Config{APIKey: "k", Model: "gpt-image-1"}); err == nil {
		t.Fatal("expected reject gpt-image-1")
	}
}

func TestProvider_New_AllowsDeprecatedMini(t *testing.T) {
	t.Parallel()
	if _, err := openai.New(openai.Config{APIKey: "k", Model: "gpt-image-1-mini"}); err != nil {
		t.Fatalf("mini should remain allowed: %v", err)
	}
}

func TestProvider_Generate_NonPNGPayload(t *testing.T) {
	t.Parallel()
	jpegSOI := []byte{0xff, 0xd8, 0xff, 0xd9}
	b64 := base64.StdEncoding.EncodeToString(jpegSOI)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1,"data":[{"b64_json":"`+b64+`"}]}`)
	}))
	defer srv.Close()
	p, err := openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes: encodePNG(t, 8, 8, false),
		PhotoName:  "x.png",
		Prompt:     "p",
	})
	if generation.ClassifyError(err) != generation.ErrorPermanent {
		t.Fatalf("err = %v", err)
	}
}

func TestProvider_Generate_RejectsGarbagePhoto(t *testing.T) {
	t.Parallel()
	p, err := openai.New(openai.Config{APIKey: "k", BaseURL: "http://127.0.0.1:9"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dog.bin", "dog.png", "dog.jpg"} {
		_, err = p.Generate(context.Background(), generation.GenerationRequest{
			PhotoBytes: []byte("not-an-image"),
			PhotoName:  name,
			Prompt:     "p",
		})
		if generation.ClassifyError(err) != generation.ErrorRejectedInput {
			t.Fatalf("name=%s err=%v", name, err)
		}
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
		PhotoBytes: encodePNG(t, 8, 8, false),
		PhotoName:  "x.png",
		Prompt:     "p",
	})
	if generation.ClassifyError(err) != generation.ErrorRateLimited {
		t.Fatalf("err = %v", err)
	}
}

func TestProvider_Generate_BadRequest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"bad image"}}`)
	}))
	defer srv.Close()
	p, err := openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes: encodePNG(t, 8, 8, false),
		PhotoName:  "x.png",
		Prompt:     "p",
	})
	if generation.ClassifyError(err) != generation.ErrorRejectedInput {
		t.Fatalf("err = %v", err)
	}
}

func TestProvider_Generate_EmptyData(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1,"data":[]}`)
	}))
	defer srv.Close()
	p, err := openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes: encodePNG(t, 8, 8, false),
		PhotoName:  "x.png",
		Prompt:     "p",
	})
	if generation.ClassifyError(err) != generation.ErrorPermanent {
		t.Fatalf("err = %v", err)
	}
}

func TestProvider_Generate_JPEGMime(t *testing.T) {
	t.Parallel()
	outPNG := encodePNG(t, 4, 4, false)
	outB64 := base64.StdEncoding.EncodeToString(outPNG)
	jpegSOI := []byte{0xff, 0xd8, 0xff, 0xd9}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		_, hdr, err := r.FormFile("image[]")
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Header.Get("Content-Type") != "image/jpeg" {
			t.Fatalf("ct = %q", hdr.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1,"data":[{"b64_json":"`+outB64+`"}]}`)
	}))
	defer srv.Close()
	p, err := openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Generate(context.Background(), generation.GenerationRequest{
		PhotoBytes: jpegSOI,
		PhotoName:  "dog.jpg",
		Prompt:     "p",
	}); err != nil {
		t.Fatal(err)
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
	_, err = p.Generate(ctx, generation.GenerationRequest{
		PhotoBytes: encodePNG(t, 8, 8, false),
		PhotoName:  "x.png",
		Prompt:     "p",
	})
	if generation.ClassifyError(err) != generation.ErrorCanceled {
		t.Fatalf("err = %v class=%s", err, generation.ClassifyError(err))
	}
}
