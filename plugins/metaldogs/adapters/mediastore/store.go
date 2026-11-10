package mediastore

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// Compile-time check.
var _ ports.AssetStore = (*Store)(nil)

// contentTypedSaver is optional on platform backends that honor MIME on write.
type contentTypedSaver interface {
	SaveWithContentType(path string, file io.Reader, contentType string) error
}

// contextReadable is optional on backends that honor ctx during Read.
type contextReadable interface {
	ReadContext(ctx context.Context, path string) ([]byte, error)
}

// previewMIMEByExt is the allowlist for inline customizer delivery.
// Anything else is stored/served as octet-stream with attachment disposition.
var previewMIMEByExt = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
}

// Store adapts Shopanda media.SignedURLStorage to ports.AssetStore and
// enforces the Metal Dogs private key namespace.
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

func rejectKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, `\`) || strings.Contains(key, "..") {
		return fmt.Errorf("mediastore: key %q rejected", key)
	}
	if path.Clean(key) != key {
		return fmt.Errorf("mediastore: key %q rejected", key)
	}
	if !strings.HasPrefix(key, ports.AssetKeyPrefix) {
		return fmt.Errorf("mediastore: key %q outside %s", key, ports.AssetKeyPrefix)
	}
	rest := strings.TrimPrefix(key, ports.AssetKeyPrefix)
	if rest == "" {
		return fmt.Errorf("mediastore: key %q rejected", key)
	}
	return nil
}

// resolveContentType maps key extension to a safe MIME. Caller-supplied types
// outside the allowlist are ignored (XSS/open-in-tab hazard with inline GETs).
func resolveContentType(key, claimed string) (ctype string, inlineOK bool) {
	ext := strings.ToLower(path.Ext(key))
	safe, ok := previewMIMEByExt[ext]
	if !ok {
		return "application/octet-stream", false
	}
	claimed = strings.ToLower(strings.TrimSpace(claimed))
	if claimed != "" && claimed != safe {
		// Keep extension-derived safe type; do not trust caller.
		return safe, true
	}
	return safe, true
}

// ctxReader stops reads when ctx is done so Save can unwind without a
// background goroutine.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	if cerr := c.ctx.Err(); cerr != nil {
		return n, cerr
	}
	return n, err
}

// Put writes via media.Storage.Save (or SaveWithContentType when available).
// The call is synchronous; ctx cancellation stops the reader so Save returns
// without leaving an abandoned write goroutine.
func (s *Store) Put(ctx context.Context, key string, r io.Reader, contentType string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := rejectKey(key); err != nil {
		return err
	}
	ctype, _ := resolveContentType(key, contentType)
	src := io.Reader(ctxReader{ctx: ctx, r: r})
	var err error
	if saver, ok := s.inner.(contentTypedSaver); ok {
		err = saver.SaveWithContentType(key, src, ctype)
	} else {
		err = s.inner.Save(key, src)
	}
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return fmt.Errorf("mediastore: put: %w", err)
	}
	return nil
}

// Read returns object bytes via media.ReadableStorage when the backend supports it.
// Prefers ReadContext when available so cancellation reaches the backend.
func (s *Store) Read(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := rejectKey(key); err != nil {
		return nil, err
	}
	if cr, ok := s.inner.(contextReadable); ok {
		b, err := cr.ReadContext(ctx, key)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return nil, cerr
			}
			return nil, fmt.Errorf("mediastore: read: %w", err)
		}
		return b, nil
	}
	readable, ok := s.inner.(media.ReadableStorage)
	if !ok {
		return nil, fmt.Errorf("mediastore: read: backend does not support ReadableStorage")
	}
	b, err := readable.Read(key)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return nil, cerr
		}
		return nil, fmt.Errorf("mediastore: read: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b, nil
}

// Head delegates to SignedURLStorage.Head.
func (s *Store) Head(ctx context.Context, key string) error {
	if err := rejectKey(key); err != nil {
		return err
	}
	if err := s.inner.Head(ctx, key); err != nil {
		return fmt.Errorf("mediastore: head: %w", err)
	}
	return nil
}

// Delete delegates to media.Storage.Delete.
func (s *Store) Delete(_ context.Context, key string) error {
	if err := rejectKey(key); err != nil {
		return err
	}
	if err := s.inner.Delete(key); err != nil {
		return fmt.Errorf("mediastore: delete: %w", err)
	}
	return nil
}

// SignedGetURL issues a short-lived platform signed URL. Inline disposition
// only for allowlisted preview image extensions; otherwise attachment.
func (s *Store) SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if err := rejectKey(key); err != nil {
		return "", err
	}
	ttl = ports.CapSignedGetTTL(ttl)
	filename := path.Base(key)
	_, inlineOK := resolveContentType(key, "")
	disp := media.DispositionAttachment
	if inlineOK {
		disp = media.DispositionInline
	}
	url, err := s.inner.SignedURL(ctx, key, media.SignURLOptions{
		TTL:         ttl,
		Filename:    filename,
		Disposition: disp,
	})
	if err != nil {
		return "", fmt.Errorf("mediastore: signed get: %w", err)
	}
	return url, nil
}
