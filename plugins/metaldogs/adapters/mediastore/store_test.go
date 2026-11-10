package mediastore_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/infrastructure/localfs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/mediastore"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

type fakeSigned struct {
	objects   map[string][]byte
	lastTTL   time.Duration
	lastDisp  media.Disposition
	lastCT    string
	saveCalls int
}

func (f *fakeSigned) Name() string { return "fake" }
func (f *fakeSigned) Save(path string, file io.Reader) error {
	return f.SaveWithContentType(path, file, "")
}
func (f *fakeSigned) SaveWithContentType(path string, file io.Reader, contentType string) error {
	f.saveCalls++
	f.lastCT = contentType
	b, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	if f.objects == nil {
		f.objects = map[string][]byte{}
	}
	f.objects[path] = b
	return nil
}
func (f *fakeSigned) Delete(path string) error {
	delete(f.objects, path)
	return nil
}
func (f *fakeSigned) URL(path string) string { return "http://example/" + path }
func (f *fakeSigned) Head(_ context.Context, path string) error {
	if _, ok := f.objects[path]; !ok {
		return os.ErrNotExist
	}
	return nil
}
func (f *fakeSigned) SignedURL(_ context.Context, path string, opts media.SignURLOptions) (string, error) {
	f.lastTTL = opts.TTL
	f.lastDisp = opts.Disposition
	if _, ok := f.objects[path]; !ok {
		return "", os.ErrNotExist
	}
	return "https://signed.example/" + path + "?sig=1", nil
}
func (f *fakeSigned) Read(path string) ([]byte, error) {
	b, ok := f.objects[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}

func TestStore_WrapsSignedURLStorage(t *testing.T) {
	t.Parallel()
	inner := &fakeSigned{}
	store, err := mediastore.New(inner)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "private/blobs/metaldogs/source/2026/11/a1.jpg"
	if err := store.Put(ctx, key, bytes.NewReader([]byte("abc")), "text/html"); err != nil {
		t.Fatal(err)
	}
	if inner.lastCT != "image/jpeg" {
		t.Fatalf("content-type = %q, want image/jpeg (override unsafe caller type)", inner.lastCT)
	}
	if err := store.Head(ctx, key); err != nil {
		t.Fatal(err)
	}
	url, err := store.SignedGetURL(ctx, key, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if url == "" || inner.lastTTL != 2*time.Minute {
		t.Fatalf("url=%q ttl=%v", url, inner.lastTTL)
	}
	if inner.lastDisp != media.DispositionInline {
		t.Fatalf("disposition = %q, want inline for jpeg", inner.lastDisp)
	}
	binKey := "private/blobs/metaldogs/source/2026/11/a1.bin"
	if err := store.Put(ctx, binKey, bytes.NewReader([]byte("bin")), "text/html"); err != nil {
		t.Fatal(err)
	}
	if inner.lastCT != "application/octet-stream" {
		t.Fatalf("bin content-type = %q", inner.lastCT)
	}
	if _, err := store.SignedGetURL(ctx, binKey, time.Minute); err != nil {
		t.Fatal(err)
	}
	if inner.lastDisp != media.DispositionAttachment {
		t.Fatalf("bin disposition = %q, want attachment", inner.lastDisp)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := store.Head(ctx, key); err == nil {
		t.Fatal("want missing after delete")
	}
}

func TestStore_RejectsKeysOutsideNamespace(t *testing.T) {
	t.Parallel()
	inner := &fakeSigned{objects: map[string][]byte{"uploads/x.jpg": []byte("x")}}
	store, err := mediastore.New(inner)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rejects := []string{
		"uploads/x.jpg",
		"private/downloads/x.pdf",
		"private/blobs/other/x.bin",
		"private/blobs/metaldogs/../other/x.bin",
		"../private/blobs/metaldogs/x.bin",
		"",
	}
	for _, key := range rejects {
		if err := store.Put(ctx, key, bytes.NewReader([]byte("x")), ""); err == nil {
			t.Fatalf("Put(%q) want reject", key)
		}
		if err := store.Head(ctx, key); err == nil {
			t.Fatalf("Head(%q) want reject", key)
		}
		if err := store.Delete(ctx, key); err == nil {
			t.Fatalf("Delete(%q) want reject", key)
		}
		if _, err := store.SignedGetURL(ctx, key, time.Minute); err == nil {
			t.Fatalf("SignedGetURL(%q) want reject", key)
		}
	}
	if inner.saveCalls != 0 {
		t.Fatalf("inner Save called %d times", inner.saveCalls)
	}
}

type blockingCtxReadable struct {
	fakeSigned
	started chan struct{}
	release chan struct{}
}

func (b *blockingCtxReadable) ReadContext(ctx context.Context, path string) ([]byte, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	select {
	case <-b.release:
		return b.fakeSigned.Read(path)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestStore_ReadRespectsCancel(t *testing.T) {
	t.Parallel()
	inner := &blockingCtxReadable{
		fakeSigned: fakeSigned{objects: map[string][]byte{
			"private/blobs/metaldogs/source/2026/11/a1.jpg": []byte("abc"),
		}},
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	store, err := mediastore.New(inner)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := store.Read(ctx, "private/blobs/metaldogs/source/2026/11/a1.jpg")
		errCh <- err
	}()
	<-inner.started
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not return on cancel")
	}
	close(inner.release)
}

type slowPutReader struct {
	started chan struct{}
	release chan struct{}
	n       int
}

func (s *slowPutReader) Read(p []byte) (int, error) {
	if s.n == 0 {
		select {
		case s.started <- struct{}{}:
		default:
		}
		<-s.release
		s.n++
		if len(p) == 0 {
			return 0, nil
		}
		p[0] = 'x'
		return 1, io.EOF
	}
	return 0, io.EOF
}

func TestStore_PutRespectsCancel(t *testing.T) {
	t.Parallel()
	inner := &fakeSigned{}
	store, err := mediastore.New(inner)
	if err != nil {
		t.Fatal(err)
	}
	r := &slowPutReader{started: make(chan struct{}, 1), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- store.Put(ctx, "private/blobs/metaldogs/source/2026/11/a1.jpg", r, "image/jpeg")
	}()
	<-r.started
	cancel()
	close(r.release)
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Put did not return on cancel")
	}
	if inner.saveCalls != 1 {
		t.Fatalf("saveCalls=%d", inner.saveCalls)
	}
	if len(inner.objects) != 0 {
		t.Fatalf("cancelled Put must not leave object: %v", inner.objects)
	}
}

func TestStore_CapsTTL(t *testing.T) {
	t.Parallel()
	inner := &fakeSigned{}
	store, _ := mediastore.New(inner)
	key := "private/blobs/metaldogs/source/2026/11/a1.jpg"
	_ = store.Put(context.Background(), key, bytes.NewReader([]byte("x")), "")
	if _, err := store.SignedGetURL(context.Background(), key, time.Hour); err != nil {
		t.Fatal(err)
	}
	if inner.lastTTL != ports.MaxSignedGetTTL {
		t.Fatalf("ttl = %v, want cap %v", inner.lastTTL, ports.MaxSignedGetTTL)
	}
	if _, err := store.SignedGetURL(context.Background(), key, 0); err != nil {
		t.Fatal(err)
	}
	if inner.lastTTL != ports.DefaultSignedGetTTL {
		t.Fatalf("ttl = %v, want default %v", inner.lastTTL, ports.DefaultSignedGetTTL)
	}
}

func TestStore_LocalfsContractRoundTrip(t *testing.T) {
	t.Parallel()
	st, err := localfs.NewWithSecret(t.TempDir(), "/media", []byte("unit-test-sign-secret-32-bytes!!"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := mediastore.New(st)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key, err := ports.ObjectKey("source", "asset-1", "jpg", time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !media.IsPrivatePath(key) || !strings.HasPrefix(key, ports.AssetKeyPrefix) {
		t.Fatalf("key = %q", key)
	}
	body := []byte("fake-jpeg-bytes")
	if err := store.Put(ctx, key, bytes.NewReader(body), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	url, err := store.SignedGetURL(ctx, key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, localfs.SignedPathPrefix) {
		t.Fatalf("url = %q", url)
	}
	rec := httptest.NewRecorder()
	st.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Fatalf("body mismatch")
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("Cache-Control = %q", cc)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "inline") {
		t.Fatalf("Content-Disposition = %q, want inline for jpg", cd)
	}

	rec = httptest.NewRecorder()
	st.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, localfs.SignedPathPrefix+"bogus", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bad token status = %d", rec.Code)
	}
}

func TestNew_NilInner(t *testing.T) {
	t.Parallel()
	if _, err := mediastore.New(nil); err == nil {
		t.Fatal("want error")
	}
}
