// Package openai implements the G1 ImageProvider candidate against OpenAI Images.
package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akarso/shopanda/plugins/metaldogs/generation"
)

const (
	Name           = "openai"
	DefaultBaseURL = "https://api.openai.com/v1"
	// DefaultModel is the current OpenAI Images edits default (gpt-image-1 shut down 2026-10-23).
	DefaultModel = "gpt-image-2.5-sunburst"
	DefaultSize  = "1024x1024"
)

// Config holds OpenAI Images credentials and transport knobs.
type Config struct {
	APIKey     string
	BaseURL    string // empty → DefaultBaseURL
	Model      string // empty → DefaultModel; overridden by req.Model when set
	Size       string // empty → DefaultSize; overridden by req.Parameters WxH when set
	HTTPClient *http.Client
}

// Provider calls OpenAI images/edits with the customer photo + prompt.
type Provider struct {
	cfg    Config
	client *http.Client
}

// New returns an OpenAI Images provider. APIKey is required.
func New(cfg Config) (*Provider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, &generation.ProviderError{Class: generation.ErrorRejectedInput, Message: "openai api key required"}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if err := rejectShutdownModel(cfg.Model); err != nil {
		return nil, err
	}
	if cfg.Size == "" {
		cfg.Size = DefaultSize
	}
	if err := validateAPISizeString(cfg.Size); err != nil {
		return nil, err
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	return &Provider{cfg: cfg, client: client}, nil
}

func (p *Provider) Name() string { return Name }

func (p *Provider) Generate(ctx context.Context, req generation.GenerationRequest) (generation.GenerationResult, error) {
	if err := ctx.Err(); err != nil {
		return generation.GenerationResult{}, err
	}
	if len(req.PhotoBytes) == 0 {
		return generation.GenerationResult{}, &generation.ProviderError{Class: generation.ErrorRejectedInput, Message: "photo bytes required"}
	}
	if len(req.PhotoBytes) > generation.MaxPhotoBytes {
		return generation.GenerationResult{}, &generation.ProviderError{
			Class:   generation.ErrorRejectedInput,
			Message: fmt.Sprintf("photo exceeds %d bytes", generation.MaxPhotoBytes),
		}
	}
	if req.Prompt == "" {
		return generation.GenerationResult{}, &generation.ProviderError{Class: generation.ErrorRejectedInput, Message: "prompt required"}
	}

	model := p.cfg.Model
	if strings.TrimSpace(req.Model) != "" {
		model = strings.TrimSpace(req.Model)
	}
	if err := rejectShutdownModel(model); err != nil {
		return generation.GenerationResult{}, err
	}
	size, err := resolveAPISize(req.Parameters, p.cfg.Size)
	if err != nil {
		return generation.GenerationResult{}, err
	}

	prompt := req.Prompt
	if req.DogName != "" {
		prompt = fmt.Sprintf("%s Dog name to render exactly: %q.", prompt, req.DogName)
	}

	body, contentType, err := buildEditMultipart(req.PhotoBytes, req.PhotoName, prompt, model, size)
	if err != nil {
		return generation.GenerationResult{}, err
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/images/edits"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return generation.GenerationResult{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	httpReq.Header.Set("Content-Type", contentType)

	start := time.Now()
	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return generation.GenerationResult{}, ctx.Err()
		}
		return generation.GenerationResult{}, &generation.ProviderError{Class: generation.ErrorTransient, Message: "openai transport: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return generation.GenerationResult{}, &generation.ProviderError{Class: generation.ErrorTransient, Message: "openai read body: " + err.Error()}
	}
	latency := time.Since(start)

	if resp.StatusCode >= 400 {
		return generation.GenerationResult{}, mapHTTPError(resp.StatusCode, raw)
	}

	png, usage, err := parseImagesResponse(raw)
	if err != nil {
		return generation.GenerationResult{}, err
	}
	reqID := strings.TrimSpace(resp.Header.Get("x-request-id"))
	if reqID == "" {
		reqID = strings.TrimSpace(resp.Header.Get("X-Request-Id"))
	}
	if reqID == "" {
		reqID = fmt.Sprintf("openai-%d", usage.Created)
	}
	hasAlpha := pngHasAlpha(png)
	return generation.GenerationResult{
		ImagePNG:          png,
		HasAlpha:          hasAlpha,
		Latency:           latency,
		CostMinorUnits:    0, // pricing conversion operator-side; tokens recorded below
		UsageInputTokens:  usage.InputTokens,
		UsageOutputTokens: usage.OutputTokens,
		UsageTotalTokens:  usage.TotalTokens,
		ProviderReqID:     reqID,
		Model:             model,
	}, nil
}

func rejectShutdownModel(model string) error {
	// gpt-image-1 shut down 2026-10-23. gpt-image-1-mini remains deprecated-but-available.
	// dall-e-2 / dall-e-3 are retired and unsupported by this GPT-image edits adapter.
	switch model {
	case "gpt-image-1":
		return &generation.ProviderError{
			Class:   generation.ErrorRejectedInput,
			Message: fmt.Sprintf("model %q shut down; use %s (or gpt-image-2.5-flare)", model, DefaultModel),
		}
	case "dall-e-2", "dall-e-3":
		return &generation.ProviderError{
			Class:   generation.ErrorRejectedInput,
			Message: fmt.Sprintf("model %q is retired and unsupported by this adapter; use %s", model, DefaultModel),
		}
	default:
		return nil
	}
}

// resolveAPISize maps template WxH → API size without applying fake DefaultDimension (64).
// Unset canvas uses config/default 1024x1024 so unpaid/paid calls never send 64x64.
func resolveAPISize(params generation.Parameters, fallback string) (string, error) {
	w, h := params.Width, params.Height
	switch {
	case w == 0 && h == 0:
		if fallback == "" {
			fallback = DefaultSize
		}
		if err := validateAPISizeString(fallback); err != nil {
			return "", err
		}
		return fallback, nil
	case w <= 0 || h <= 0:
		return "", &generation.ProviderError{
			Class:   generation.ErrorRejectedInput,
			Message: fmt.Sprintf("openai size: both width and height required (got %dx%d)", w, h),
		}
	default:
		if err := validateAPIDims(w, h); err != nil {
			return "", err
		}
		return fmt.Sprintf("%dx%d", w, h), nil
	}
}

const (
	// minAPIEdge is a local soft floor (not an OpenAI hard limit) to avoid
	// tiny paid 400s. GPT image 2.5 also requires edges ÷16 and aspect 1:3..3:1;
	// hard cap matches generation.MaxDimension.
	minAPIEdge = 256
)

func validateAPISizeString(size string) error {
	size = strings.TrimSpace(size)
	if size == "auto" {
		return nil
	}
	var w, h int
	n, err := fmt.Sscanf(size, "%dx%d", &w, &h)
	if err != nil || n != 2 || fmt.Sprintf("%dx%d", w, h) != size {
		return &generation.ProviderError{
			Class:   generation.ErrorRejectedInput,
			Message: fmt.Sprintf("openai size %q invalid (want WIDTHxHEIGHT or auto)", size),
		}
	}
	return validateAPIDims(w, h)
}

func validateAPIDims(w, h int) error {
	if w < minAPIEdge || h < minAPIEdge {
		return &generation.ProviderError{
			Class:   generation.ErrorRejectedInput,
			Message: fmt.Sprintf("openai size %dx%d below minimum %d", w, h, minAPIEdge),
		}
	}
	if w > generation.MaxDimension || h > generation.MaxDimension {
		return &generation.ProviderError{
			Class:   generation.ErrorRejectedInput,
			Message: fmt.Sprintf("openai size %dx%d exceeds max %d", w, h, generation.MaxDimension),
		}
	}
	if w%16 != 0 || h%16 != 0 {
		return &generation.ProviderError{
			Class:   generation.ErrorRejectedInput,
			Message: fmt.Sprintf("openai size %dx%d: edges must be divisible by 16", w, h),
		}
	}
	// Aspect ratio must be between 1:3 and 3:1 inclusive.
	if w*3 < h || h*3 < w {
		return &generation.ProviderError{
			Class:   generation.ErrorRejectedInput,
			Message: fmt.Sprintf("openai size %dx%d: aspect must be between 1:3 and 3:1", w, h),
		}
	}
	return nil
}

func buildEditMultipart(photo []byte, photoName, prompt, model, size string) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("model", model); err != nil {
		return nil, "", err
	}
	if err := w.WriteField("prompt", prompt); err != nil {
		return nil, "", err
	}
	if err := w.WriteField("size", size); err != nil {
		return nil, "", err
	}
	// GPT image models reject response_format; they always return b64_json.
	if err := w.WriteField("background", "transparent"); err != nil {
		return nil, "", err
	}
	if err := w.WriteField("output_format", "png"); err != nil {
		return nil, "", err
	}

	filename, mime, err := imagePartMeta(photo, photoName)
	if err != nil {
		return nil, "", err
	}
	h := make(textproto.MIMEHeader)
	// GPT image edits expect repeated image[] parts (docs curl examples).
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image[]"; filename="%s"`, filename))
	h.Set("Content-Type", mime)
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(photo); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
}

func imagePartMeta(photo []byte, photoName string) (filename, mime string, err error) {
	mime = sniffImageMIME(photo)
	if mime == "" {
		return "", "", &generation.ProviderError{
			Class:   generation.ErrorRejectedInput,
			Message: "openai: photo must be PNG, JPEG, or WebP (magic bytes)",
		}
	}
	filename = sanitizeFilename(photoName)
	if filename == "" {
		switch mime {
		case "image/jpeg":
			filename = "photo.jpg"
		case "image/webp":
			filename = "photo.webp"
		default:
			filename = "photo.png"
		}
	}
	return filename, mime, nil
}

func sanitizeFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	base = strings.ReplaceAll(base, `"`, "")
	base = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\x00' {
			return -1
		}
		return r
	}, base)
	if base == "." || base == ".." {
		return ""
	}
	return base
}

// sniffImageMIME accepts only magic bytes. Extension is never used for accept/reject
// (mislabeled garbage must not become image/png).
func sniffImageMIME(photo []byte) string {
	if len(photo) >= 8 && bytes.Equal(photo[:8], []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}) {
		return "image/png"
	}
	if len(photo) >= 3 && photo[0] == 0xff && photo[1] == 0xd8 && photo[2] == 0xff {
		return "image/jpeg"
	}
	if len(photo) >= 12 && string(photo[0:4]) == "RIFF" && string(photo[8:12]) == "WEBP" {
		return "image/webp"
	}
	return ""
}

type imagesResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		B64JSON string `json:"b64_json"`
	} `json:"data"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

type parsedUsage struct {
	Created      int64
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

func parseImagesResponse(raw []byte) ([]byte, parsedUsage, error) {
	var parsed imagesResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, parsedUsage{}, &generation.ProviderError{Class: generation.ErrorPermanent, Message: "openai decode: " + err.Error()}
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, parsedUsage{}, &generation.ProviderError{Class: generation.ErrorPermanent, Message: "openai: " + parsed.Error.Message}
	}
	if len(parsed.Data) == 0 || parsed.Data[0].B64JSON == "" {
		return nil, parsedUsage{}, &generation.ProviderError{Class: generation.ErrorPermanent, Message: "openai: empty image data"}
	}
	decoded, err := base64.StdEncoding.DecodeString(parsed.Data[0].B64JSON)
	if err != nil {
		return nil, parsedUsage{}, &generation.ProviderError{Class: generation.ErrorPermanent, Message: "openai b64: " + err.Error()}
	}
	if !bytes.HasPrefix(decoded, []byte{0x89, 0x50, 0x4e, 0x47}) {
		return nil, parsedUsage{}, &generation.ProviderError{Class: generation.ErrorPermanent, Message: "openai: decoded payload is not PNG"}
	}
	u := parsedUsage{Created: parsed.Created}
	if parsed.Usage != nil {
		u.InputTokens = parsed.Usage.InputTokens
		u.OutputTokens = parsed.Usage.OutputTokens
		u.TotalTokens = parsed.Usage.TotalTokens
	}
	return decoded, u, nil
}

// pngHasAlpha reports whether any decoded pixel has useful transparency
// (a < 0xffff), including RGB+tRNS. Colour type alone is not enough: opaque
// RGBA must be false; type 0/2 with tRNS must be true.
func pngHasAlpha(pngBytes []byte) bool {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return false
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a < 0xffff {
				return true
			}
		}
	}
	return false
}

func mapHTTPError(status int, raw []byte) error {
	msg := truncateUTF8(strings.TrimSpace(string(raw)), 256)
	var parsed imagesResponse
	if json.Unmarshal(raw, &parsed) == nil && parsed.Error != nil && parsed.Error.Message != "" {
		msg = parsed.Error.Message
	}
	switch {
	case status == http.StatusTooManyRequests:
		return &generation.ProviderError{Class: generation.ErrorRateLimited, Message: msg}
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		return &generation.ProviderError{Class: generation.ErrorRejectedInput, Message: msg}
	case status >= 500:
		return &generation.ProviderError{Class: generation.ErrorTransient, Message: fmt.Sprintf("openai http %d: %s", status, msg)}
	default:
		return &generation.ProviderError{Class: generation.ErrorPermanent, Message: fmt.Sprintf("openai http %d: %s", status, msg)}
	}
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.ValidString(s[:maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes] + "…"
}
