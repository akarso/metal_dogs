package ports

import (
	"context"
	"io"

	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// MediaBackend is the signed object store the mediastore adapter wraps.
// Read and SaveWithContentType stay off this interface; the adapter type-asserts
// them on the concrete backend (localfs and S3 implement both).
type MediaBackend interface {
	Name() string
	Save(path string, file io.Reader) error
	Delete(path string) error
	URL(path string) string
	Head(ctx context.Context, path string) error
	SignedURL(ctx context.Context, path string, opts pluginsdk.SignURLOptions) (string, error)
}
