package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

type memAssetRepo struct {
	mu   sync.Mutex
	byID map[string]domain.Asset
}

func (m *memAssetRepo) Insert(_ context.Context, a domain.Asset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byID == nil {
		m.byID = map[string]domain.Asset{}
	}
	m.byID[a.ID] = a
	return nil
}

func (m *memAssetRepo) GetByID(_ context.Context, id string) (domain.Asset, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[id]
	if !ok || a.DeletedAt != nil {
		return domain.Asset{}, false, nil
	}
	return a, true, nil
}

type memBlobStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (m *memBlobStore) Put(_ context.Context, key string, r io.Reader, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.objects == nil {
		m.objects = map[string][]byte{}
	}
	m.objects[key] = b
	return nil
}
func (m *memBlobStore) Head(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objects[key]; !ok {
		return fmt.Errorf("missing")
	}
	return nil
}
func (m *memBlobStore) SignedGetURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://signed.example/" + key, nil
}
func (m *memBlobStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

var _ ports.AssetStore = (*memBlobStore)(nil)
var _ ports.AssetRepository = (*memAssetRepo)(nil)

func jpegBytes(t *testing.T, n int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			img.Set(x, y, color.RGBA{R: 1, G: 2, B: 3, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func multipartBody(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func testSvc(t *testing.T) *application.UploadService {
	t.Helper()
	return &application.UploadService{
		Assets: &memAssetRepo{},
		Store:  &memBlobStore{},
		IDs:    testutil.NewSeqGenerator("up"),
		Clock:  testutil.NewFakeClock(time.Date(2026, 11, 9, 15, 0, 0, 0, time.UTC)),
	}
}

func TestCreateUpload_HappyPath(t *testing.T) {
	t.Parallel()
	body, ct := multipartBody(t, "file", "dog.jpg", jpegBytes(t, 320))
	req := httptest.NewRequest(http.MethodPost, "/api/customizer/uploads", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	httpapi.CreateUpload(testSvc(t)).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got httpapi.UploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.UploadID == "" || got.PreviewURL == "" || got.Width != 320 || got.MimeType != "image/jpeg" {
		t.Fatalf("dto=%+v", got)
	}
	if got.ExpiresAt.IsZero() {
		t.Fatal("expires_at required")
	}
}

func TestCreateUpload_WrongMIMEWithJPGName(t *testing.T) {
	t.Parallel()
	body, ct := multipartBody(t, "file", "dog.jpg", []byte("definitely-not-an-image"))
	req := httptest.NewRequest(http.MethodPost, "/api/customizer/uploads", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	httpapi.CreateUpload(testSvc(t)).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertErrCode(t, rec, application.UploadErrUnsupportedMedia)
}

func TestCreateUpload_MissingFile(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("other", "x")
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/customizer/uploads", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	httpapi.CreateUpload(testSvc(t)).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertErrCode(t, rec, "bad_request")
}

func TestCreateUpload_NilService(t *testing.T) {
	t.Parallel()
	body, ct := multipartBody(t, "file", "dog.jpg", jpegBytes(t, 300))
	req := httptest.NewRequest(http.MethodPost, "/api/customizer/uploads", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	httpapi.CreateUpload(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
	assertErrCode(t, rec, application.UploadErrUnavailable)
}

func assertErrCode(t *testing.T, rec *httptest.ResponseRecorder, code string) {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != code {
		t.Fatalf("error=%q want %q", body["error"], code)
	}
}
