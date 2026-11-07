package localfs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// Compile-time check.
var _ ports.AssetStore = (*AssetStore)(nil)

// DevAssetsPath is the public route pattern for the local signed-GET shim.
const DevAssetsPath = "/api/customizer/dev-assets/"

// AssetStore stores objects under a private filesystem root and issues
// HMAC-signed URLs served by Handler (ADR-0004 local shim).
type AssetStore struct {
	root   string
	secret []byte
	base   string // absolute URL prefix for SignedGetURL, e.g. "" for relative
}

// NewAssetStore creates a local AssetStore. secret must be non-empty.
// baseURL is the origin prefix prepended to DevAssetsPath tokens (may be empty
// for relative URLs).
func NewAssetStore(root string, secret []byte, baseURL string) (*AssetStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("localfs: asset store root required")
	}
	if len(secret) == 0 {
		return nil, fmt.Errorf("localfs: asset store signing secret required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("localfs: abs root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("localfs: mkdir root: %w", err)
	}
	return &AssetStore{
		root:   abs,
		secret: append([]byte(nil), secret...),
		base:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
	}, nil
}

// Root returns the absolute private storage root.
func (s *AssetStore) Root() string { return s.root }

func (s *AssetStore) resolve(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("localfs: invalid key %q", key)
	}
	full := filepath.Join(s.root, filepath.FromSlash(key))
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("localfs: resolve: %w", err)
	}
	if abs != s.root && !strings.HasPrefix(abs, s.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("localfs: path escapes root")
	}
	return abs, nil
}

// Put writes object bytes at key. contentType is accepted for port parity and ignored.
func (s *AssetStore) Put(_ context.Context, key string, r io.Reader, _ string) error {
	full, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return fmt.Errorf("localfs: mkdir: %w", err)
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("localfs: create: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		_ = os.Remove(full)
		return fmt.Errorf("localfs: write: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("localfs: close: %w", err)
	}
	return nil
}

// Head reports whether key exists.
func (s *AssetStore) Head(_ context.Context, key string) error {
	full, err := s.resolve(key)
	if err != nil {
		return err
	}
	_, err = os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("localfs: head %q: %w", key, os.ErrNotExist)
		}
		return fmt.Errorf("localfs: head %q: %w", key, err)
	}
	return nil
}

// Delete removes key if present.
func (s *AssetStore) Delete(_ context.Context, key string) error {
	full, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("localfs: delete: %w", err)
	}
	return nil
}

// SignedGetURL returns a short-lived HMAC URL for the local shim handler.
func (s *AssetStore) SignedGetURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	if _, err := s.resolve(key); err != nil {
		return "", err
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	exp := time.Now().UTC().Add(ttl).Unix()
	token, err := s.sign(key, exp)
	if err != nil {
		return "", err
	}
	path := DevAssetsPath + token
	if s.base == "" {
		return path, nil
	}
	return s.base + path, nil
}

// ExpiredTokenForTest returns an HMAC token that is already past expiry.
func (s *AssetStore) ExpiredTokenForTest(key string) (string, error) {
	if _, err := s.resolve(key); err != nil {
		return "", err
	}
	return s.sign(key, time.Now().UTC().Add(-time.Minute).Unix())
}

func (s *AssetStore) sign(key string, exp int64) (string, error) {
	payload := make([]byte, 8+len(key))
	binary.BigEndian.PutUint64(payload[:8], uint64(exp))
	copy(payload[8:], key)
	mac := hmac.New(sha256.New, s.secret)
	if _, err := mac.Write(payload); err != nil {
		return "", err
	}
	sum := mac.Sum(nil)
	raw := append(payload, sum...)
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// ParseToken validates token and returns the object key when still valid.
func (s *AssetStore) ParseToken(token string) (key string, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", fmt.Errorf("localfs: token decode: %w", err)
	}
	const macLen = sha256.Size
	if len(raw) < 8+macLen+1 {
		return "", fmt.Errorf("localfs: token too short")
	}
	payload := raw[:len(raw)-macLen]
	sum := raw[len(raw)-macLen:]
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(sum, mac.Sum(nil)) {
		return "", fmt.Errorf("localfs: bad mac")
	}
	exp := int64(binary.BigEndian.Uint64(payload[:8]))
	if time.Now().UTC().Unix() > exp {
		return "", fmt.Errorf("localfs: token expired")
	}
	key = string(payload[8:])
	if _, err := s.resolve(key); err != nil {
		return "", err
	}
	return key, nil
}

// Open returns a read-only file for key (used by the shim handler).
func (s *AssetStore) Open(key string) (*os.File, error) {
	full, err := s.resolve(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("localfs: open %q: %w", key, os.ErrNotExist)
		}
		return nil, fmt.Errorf("localfs: open %q: %w", key, err)
	}
	return f, nil
}
