package localfs_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/adapters/localfs"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestAssetStore_Contract(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := localfs.NewAssetStore(root, []byte("test-secret-32-bytes-minimum!!"), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key, err := ports.ObjectKey("source", "asset1", "bin", time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("hello-private")
	if err := store.Put(ctx, key, bytes.NewReader(body), "application/octet-stream"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Head(ctx, key); err != nil {
		t.Fatalf("Head: %v", err)
	}
	url, err := store.SignedGetURL(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("SignedGetURL: %v", err)
	}
	if !strings.HasPrefix(url, localfs.DevAssetsPath) {
		t.Fatalf("url = %q, want prefix %s", url, localfs.DevAssetsPath)
	}

	token := strings.TrimPrefix(url, localfs.DevAssetsPath)
	h := httpapi.DevAssets(store)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, localfs.DevAssetsPath+token, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, body) {
		t.Fatalf("body = %q", got)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("Cache-Control = %q", cc)
	}

	// Bad MAC
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, localfs.DevAssetsPath+"AAAA", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bad token status = %d", rec.Code)
	}

	// Expired token
	expired, err := store.ExpiredTokenForTest(key)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, localfs.DevAssetsPath+expired, nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expired status = %d", rec.Code)
	}
	// Missing object after delete
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := store.Head(ctx, key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Head after delete = %v", err)
	}
	url2, err := store.SignedGetURL(ctx, key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url2, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", rec.Code)
	}
}

func TestAssetStore_RejectsPathEscape(t *testing.T) {
	t.Parallel()
	store, err := localfs.NewAssetStore(t.TempDir(), []byte("secret"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "../escape", bytes.NewReader([]byte("x")), ""); err == nil {
		t.Fatal("want path escape error")
	}
}

func TestAssetStore_RequiresSecret(t *testing.T) {
	t.Parallel()
	if _, err := localfs.NewAssetStore(t.TempDir(), nil, ""); err == nil {
		t.Fatal("want secret required")
	}
}

func TestDevAssets_RoundTripRelativePath(t *testing.T) {
	t.Parallel()
	store, err := localfs.NewAssetStore(t.TempDir(), []byte("another-secret-value!!!!"), "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	key := path.Join("metaldogs", "preview", "2026", "11", "p1.bin")
	if err := store.Put(context.Background(), key, strings.NewReader("data"), ""); err != nil {
		t.Fatal(err)
	}
	u, err := store.SignedGetURL(context.Background(), key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u, "http://localhost:8080"+localfs.DevAssetsPath) {
		t.Fatalf("url = %q", u)
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	httpapi.DevAssets(store).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	got, _ := io.ReadAll(rec.Body)
	if string(got) != "data" {
		t.Fatalf("body = %q", got)
	}
}
