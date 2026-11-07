package mediastore_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/mediastore"
)

type fakeSigned struct {
	objects map[string][]byte
	lastTTL time.Duration
}

func (f *fakeSigned) Name() string { return "fake" }
func (f *fakeSigned) Save(path string, file io.Reader) error {
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
	if _, ok := f.objects[path]; !ok {
		return "", os.ErrNotExist
	}
	return "https://signed.example/" + path + "?sig=1", nil
}

func TestStore_WrapsSignedURLStorage(t *testing.T) {
	t.Parallel()
	inner := &fakeSigned{}
	store, err := mediastore.New(inner)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "metaldogs/source/2026/11/a1.bin"
	if err := store.Put(ctx, key, bytes.NewReader([]byte("abc")), "application/octet-stream"); err != nil {
		t.Fatal(err)
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
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := store.Head(ctx, key); err == nil {
		t.Fatal("want missing after delete")
	}
}

func TestNew_NilInner(t *testing.T) {
	t.Parallel()
	if _, err := mediastore.New(nil); err == nil {
		t.Fatal("want error")
	}
}
