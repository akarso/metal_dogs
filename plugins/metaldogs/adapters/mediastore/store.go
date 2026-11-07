package mediastore

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// Compile-time check.
var _ ports.AssetStore = (*Store)(nil)

// Store adapts Shopanda media.SignedURLStorage to ports.AssetStore.
type Store struct {
	inner media.SignedURLStorage
}

// New wraps a SignedURLStorage. inner must be non-nil.
func New(inner media.SignedURLStorage) (*Store, error) {
	if inner == nil {
		return nil, fmt.Errorf("mediastore: SignedURLStorage required")
	}
	return &Store{inner: inner}, nil
}

// Put writes via media.Storage.Save. contentType is ignored (platform Save has no MIME arg).
func (s *Store) Put(_ context.Context, key string, r io.Reader, _ string) error {
	if err := s.inner.Save(key, r); err != nil {
		return fmt.Errorf("mediastore: put: %w", err)
	}
	return nil
}

// Head delegates to SignedURLStorage.Head.
func (s *Store) Head(ctx context.Context, key string) error {
	if err := s.inner.Head(ctx, key); err != nil {
		return fmt.Errorf("mediastore: head: %w", err)
	}
	return nil
}

// Delete delegates to media.Storage.Delete.
func (s *Store) Delete(_ context.Context, key string) error {
	if err := s.inner.Delete(key); err != nil {
		return fmt.Errorf("mediastore: delete: %w", err)
	}
	return nil
}

// SignedGetURL issues a short-lived platform signed URL.
func (s *Store) SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	url, err := s.inner.SignedURL(ctx, key, media.SignURLOptions{TTL: ttl})
	if err != nil {
		return "", fmt.Errorf("mediastore: signed get: %w", err)
	}
	return url, nil
}
