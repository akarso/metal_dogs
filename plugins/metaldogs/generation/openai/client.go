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

	params := req.Parameters
	params.Normalize()
	if err := params.Validate(); err != nil {
		return generation.GenerationResult{}, err
	}

	model := p.cfg.Model
	if strings.TrimSpace(req.Model) != "" && req.Model != "unset" {
		model = req.Model
	}
	if err := rejectShutdownModel(model); err != nil {
		return generation.GenerationResult{}, err
	}
	size := sizeFromParams(params, p.cfg.Size)

	prompt := req.Prompt
	if req.DogName != "" {
		prompt = fmt.Sprintf("%s Dog name to render exactly: %q.", prompt, req.DogName)
	}

	body, contentType, err := buildEditMultipart(req.PhotoBytes, req.PhotoName, prompt, model, size)
	if err != nil {
		return generation.GenerationResult{}, &generation.ProviderError{Class: generation.ErrorPermanent, Message: err.Error()}
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
	switch model {
	case "gpt-image-1", "gpt-image-1-mini":
		return &generation.ProviderError{
			Class:   generation.ErrorRejectedInput,
			Message: fmt.Sprintf("model %q shut down; use %s (or gpt-image-2.5-flare)", model, DefaultModel),
		}
	default:
		return nil
	}
}

func sizeFromParams(params generation.Parameters, fallback string) string {
	if params.Width > 0 && params.Height > 0 {
		return fmt.Sprintf("%dx%d", params.Width, params.Height)
	}
	if fallback != "" {
		return fallback
	}
	return DefaultSize
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

	filename, mime := imagePartMeta(photo, photoName)
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

func imagePartMeta(photo []byte, photoName string) (filename, mime string) {
	filename = sanitizeFilename(photoName)
	mime = sniffImageMIME(photo, filename)
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
	return filename, mime
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

func sniffImageMIME(photo []byte, filename string) string {
	if len(photo) >= 8 && bytes.Equal(photo[:8], []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}) {
		return "image/png"
	}
	if len(photo) >= 3 && photo[0] == 0xff && photo[1] == 0xd8 && photo[2] == 0xff {
		return "image/jpeg"
	}
	if len(photo) >= 12 && string(photo[0:4]) == "RIFF" && string(photo[8:12]) == "WEBP" {
		return "image/webp"
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".png":
		return "image/png"
	}
	return "image/png"
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
