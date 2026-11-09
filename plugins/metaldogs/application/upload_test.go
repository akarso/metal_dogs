package application_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
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
	if _, ok := m.byID[a.ID]; ok {
		return fmt.Errorf("duplicate")
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objects[key]; !ok {
		return "", fmt.Errorf("missing")
	}
	return "https://signed.example/" + key + "?sig=1", nil
}

func (m *memBlobStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

var _ ports.AssetStore = (*memBlobStore)(nil)
var _ ports.AssetRepository = (*memAssetRepo)(nil)

func jpegN(t *testing.T, n int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUploadService_Create(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	svc := &application.UploadService{
		Assets: &memAssetRepo{},
		Store:  &memBlobStore{},
		IDs:    testutil.NewSeqGenerator("up"),
		Clock:  testutil.NewFakeClock(now),
	}
	res, err := svc.Create(context.Background(), jpegN(t, 300), "ignored.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if res.UploadID != "up-0001" || res.Width != 300 || res.Height != 300 || res.MimeType != "image/jpeg" {
		t.Fatalf("result = %+v", res)
	}
	if res.ExpiresAt != now.Add(application.UploadRetention) {
		t.Fatalf("expires_at = %v", res.ExpiresAt)
	}
	if res.PreviewURL == "" || !bytes.Contains([]byte(res.PreviewURL), []byte("signed.example")) {
		t.Fatalf("preview_url = %q", res.PreviewURL)
	}
	got, ok, err := svc.Assets.GetByID(context.Background(), res.UploadID)
	if err != nil || !ok || got.StorageKey == "" || got.Type != domain.AssetTypeSource {
		t.Fatalf("repo: %+v ok=%v err=%v", got, ok, err)
	}
	if err := svc.Store.Head(context.Background(), got.StorageKey); err != nil {
		t.Fatal(err)
	}
}

func TestUploadService_NilDeps(t *testing.T) {
	t.Parallel()
	_, err := (*application.UploadService)(nil).Create(context.Background(), jpegN(t, 300), "")
	var ue *application.UploadError
	if err == nil || !asUploadErr(err, &ue) || ue.Code != application.UploadErrUnavailable {
		t.Fatalf("err=%v", err)
	}
}

func asUploadErr(err error, target **application.UploadError) bool {
	if ue, ok := err.(*application.UploadError); ok {
		*target = ue
		return true
	}
	return false
}
