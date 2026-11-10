package metaldogs

import (
	"context"
	"database/sql"
	"io"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

type stubAssetStore struct{}

func (stubAssetStore) Put(context.Context, string, io.Reader, string) error { return nil }
func (stubAssetStore) Read(context.Context, string) ([]byte, error)         { return nil, nil }
func (stubAssetStore) Head(context.Context, string) error                   { return nil }
func (stubAssetStore) SignedGetURL(context.Context, string, time.Duration) (string, error) {
	return "", nil
}
func (stubAssetStore) Delete(context.Context, string) error { return nil }

var _ ports.AssetStore = stubAssetStore{}

func TestNewUploadService_WiresCleanupHook(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("pgx", "postgres://shopanda:unused@127.0.0.1:1/shopanda?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	log := logger.NewWithWriter(io.Discard, "error")
	svc := newUploadService(stubAssetStore{}, db, log)
	if svc == nil {
		t.Fatal("newUploadService returned nil")
	}
	if svc.OnCleanupError == nil {
		t.Fatal("OnCleanupError must be set when logger is non-nil")
	}

	if newUploadService(stubAssetStore{}, db, nil).OnCleanupError != nil {
		t.Fatal("OnCleanupError must stay nil without logger")
	}
	if newUploadService(nil, db, log) != nil || newUploadService(stubAssetStore{}, nil, log) != nil {
		t.Fatal("nil store or db must yield nil service")
	}
}
