// Package openai implements the G1 ImageProvider candidate against OpenAI Images.
package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/generation"
)

const (
	Name           = "openai"
	DefaultBaseURL = "https://api.openai.com/v1"
	DefaultModel   = "gpt-image-1"
	DefaultSize    = "1024x1024"
)

// Config holds OpenAI Images credentials and transport knobs.
type Config struct {
	APIKey     string
	BaseURL    string // empty → DefaultBaseURL
	Model      string // empty → DefaultModel
	Size       string // empty → DefaultSize (API size string)
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

	prompt := req.Prompt
	if req.DogName != "" {
		prompt = fmt.Sprintf("%s Dog name to render exactly: %q.", prompt, req.DogName)
	}

	body, contentType, err := buildEditMultipart(req.PhotoBytes, req.PhotoName, prompt, p.cfg.Model, p.cfg.Size)
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

	png, reqID, err := parseImagesResponse(raw)
	if err != nil {
		return generation.GenerationResult{}, err
	}
	return generation.GenerationResult{
		ImagePNG:       png,
		HasAlpha:       false, // OpenAI PNG may be opaque; G3 decides removal
		Latency:        latency,
		CostMinorUnits: 0, // filled when usage accounting is available
		ProviderReqID:  reqID,
		Model:          p.cfg.Model,
	}, nil
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
	if err := w.WriteField("response_format", "b64_json"); err != nil {
		return nil, "", err
	}
	filename := photoName
	if filename == "" {
		filename = "photo.jpg"
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename="%s"`, filename))
	h.Set("Content-Type", "application/octet-stream")
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

type imagesResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		B64JSON string `json:"b64_json"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func parseImagesResponse(raw []byte) ([]byte, string, error) {
	var parsed imagesResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, "", &generation.ProviderError{Class: generation.ErrorPermanent, Message: "openai decode: " + err.Error()}
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, "", &generation.ProviderError{Class: generation.ErrorPermanent, Message: "openai: " + parsed.Error.Message}
	}
	if len(parsed.Data) == 0 || parsed.Data[0].B64JSON == "" {
		return nil, "", &generation.ProviderError{Class: generation.ErrorPermanent, Message: "openai: empty image data"}
	}
	decoded, err := base64.StdEncoding.DecodeString(parsed.Data[0].B64JSON)
	if err != nil {
		return nil, "", &generation.ProviderError{Class: generation.ErrorPermanent, Message: "openai b64: " + err.Error()}
	}
	reqID := fmt.Sprintf("openai-%d", parsed.Created)
	return decoded, reqID, nil
}

func mapHTTPError(status int, raw []byte) error {
	msg := strings.TrimSpace(string(raw))
	if len(msg) > 256 {
		msg = msg[:256] + "…"
	}
	// Prefer API error.message when present; never log full payloads upstream.
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
