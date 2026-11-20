package metaldogs

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// lazyAssetStore calls Host.Media on each use. BindMediaStorage runs after InitAll,
// so Init cannot see the backend Host.Media will return.
type lazyAssetStore struct {
	host pluginsdk.Host
}

func (s lazyAssetStore) open() (ports.AssetStore, error) {
	if s.host == nil {
		return nil, fmt.Errorf("metaldogs: media storage not registered (enable core storagelocal or storages3)")
	}
	media, ok := s.host.Media()
	if !ok || media == nil {
		return nil, fmt.Errorf("metaldogs: media storage not registered (enable core storagelocal or storages3)")
	}
	signed, ok := media.AsSignedURLStorage()
	if !ok || signed == nil {
		return nil, fmt.Errorf("metaldogs: media storage %q does not implement SignedURLStorage", media.Name())
	}
	backend, ok := signed.(ports.MediaBackend)
	if !ok {
		return nil, fmt.Errorf("metaldogs: media storage %q does not implement metaldogs MediaBackend", media.Name())
	}
	return resolveAssetStore(backend)
}

func (s lazyAssetStore) Put(ctx context.Context, key string, r io.Reader, contentType string) error {
	st, err := s.open()
	if err != nil {
		return err
	}
	return st.Put(ctx, key, r, contentType)
}

func (s lazyAssetStore) Read(ctx context.Context, key string) ([]byte, error) {
	st, err := s.open()
	if err != nil {
		return nil, err
	}
	return st.Read(ctx, key)
}

func (s lazyAssetStore) Head(ctx context.Context, key string) error {
	st, err := s.open()
	if err != nil {
		return err
	}
	return st.Head(ctx, key)
}

func (s lazyAssetStore) SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	st, err := s.open()
	if err != nil {
		return "", err
	}
	return st.SignedGetURL(ctx, key, ttl)
}

func (s lazyAssetStore) Delete(ctx context.Context, key string) error {
	st, err := s.open()
	if err != nil {
		return err
	}
	return st.Delete(ctx, key)
}
