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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/generation"
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

func (m *memAssetRepo) SoftDelete(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[id]
	if !ok {
		return nil
	}
	a.DeletedAt = &at
	m.byID[id] = a
	return nil
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

func postUpload(t *testing.T, svc *application.UploadService, field, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartBody(t, field, filename, data)
	req := httptest.NewRequest(http.MethodPost, "/api/customizer/uploads", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	httpapi.CreateUpload(svc).ServeHTTP(rec, req)
	return rec
}

func TestCreateUpload_HappyPath(t *testing.T) {
	t.Parallel()
	rec := postUpload(t, testSvc(t), "file", "dog.jpg", jpegBytes(t, 320))
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
	if got.PreviewURLExpiresAt.IsZero() || got.DeleteAfter.IsZero() {
		t.Fatalf("ttl fields missing: %+v", got)
	}
	if !got.DeleteAfter.After(got.PreviewURLExpiresAt) {
		t.Fatalf("delete_after should outlive preview TTL: %+v", got)
	}
}

func TestCreateUpload_WrongMIMEWithJPGName(t *testing.T) {
	t.Parallel()
	rec := postUpload(t, testSvc(t), "file", "dog.jpg", []byte("definitely-not-an-image"))
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertErrCode(t, rec, application.UploadErrUnsupportedMedia)
}

func TestCreateUpload_WebP(t *testing.T) {
	t.Parallel()
	webp := make([]byte, 12)
	copy(webp[0:4], "RIFF")
	copy(webp[8:12], "WEBP")
	rec := postUpload(t, testSvc(t), "file", "dog.webp", webp)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertErrCode(t, rec, application.UploadErrUnsupportedMedia)
}

func TestCreateUpload_Dimensions(t *testing.T) {
	t.Parallel()
	rec := postUpload(t, testSvc(t), "file", "tiny.jpg", jpegBytes(t, 100))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertErrCode(t, rec, application.UploadErrDimensionsOutOfRange)
}

func TestCreateUpload_CorruptDecode(t *testing.T) {
	t.Parallel()
	raw := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0, 1}
	rec := postUpload(t, testSvc(t), "file", "bad.jpg", raw)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertErrCode(t, rec, application.UploadErrInvalidImage)
}

func TestCreateUpload_PayloadTooLarge(t *testing.T) {
	t.Parallel()
	// Oversized multipart body triggers MaxBytesReader → 413.
	huge := bytes.Repeat([]byte("a"), generation.MaxPhotoBytes+uploadSlackForTest())
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "big.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(huge); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/customizer/uploads", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	httpapi.CreateUpload(testSvc(t)).ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertErrCode(t, rec, application.UploadErrPayloadTooLarge)
}

func uploadSlackForTest() int { return 2 << 20 }

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
	rec := postUpload(t, nil, "file", "dog.jpg", jpegBytes(t, 300))
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
		t.Fatalf("error=%q want %q body=%s", body["error"], code, strings.TrimSpace(rec.Body.String()))
	}
}
