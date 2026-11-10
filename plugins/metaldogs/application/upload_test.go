package application_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

type memAssetRepo struct {
	mu            sync.Mutex
	byID          map[string]domain.Asset
	insertErr     error
	softDeleteErr error
}

func (m *memAssetRepo) Insert(_ context.Context, a domain.Asset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.insertErr != nil {
		return m.insertErr
	}
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

func (m *memAssetRepo) SoftDelete(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.softDeleteErr != nil {
		return m.softDeleteErr
	}
	a, ok := m.byID[id]
	if !ok {
		return nil
	}
	a.DeletedAt = &at
	m.byID[id] = a
	return nil
}

func (m *memAssetRepo) BindOwnerRef(_ context.Context, assetID, ownerRef string, deleteAfter time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[assetID]
	if !ok || a.DeletedAt != nil || a.Type != domain.AssetTypeSource || a.OwnerRef != "" {
		return ports.ErrAssetBindConflict
	}
	a.OwnerRef = ownerRef
	a.DeleteAfter = deleteAfter
	m.byID[assetID] = a
	return nil
}

func (m *memAssetRepo) ClearOwnerRef(_ context.Context, assetID, ownerRef string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[assetID]
	if !ok || a.OwnerRef != ownerRef || a.DeletedAt != nil {
		return ports.ErrAssetBindConflict
	}
	a.OwnerRef = ""
	m.byID[assetID] = a
	return nil
}
func (m *memAssetRepo) ClearStorageKey(_ context.Context, assetID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[assetID]
	if !ok {
		return nil
	}
	a.StorageKey = ""
	m.byID[assetID] = a
	return nil
}

type memBlobStore struct {
	mu        sync.Mutex
	objects   map[string][]byte
	signErr   error
	deleteErr error
	deletes   int
	putCalls  int
}

func (m *memBlobStore) Put(_ context.Context, key string, r io.Reader, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.putCalls++
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
	if m.signErr != nil {
		return "", m.signErr
	}
	if _, ok := m.objects[key]; !ok {
		return "", fmt.Errorf("missing")
	}
	return "https://signed.example/" + key + "?sig=1", nil
}

func (m *memBlobStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletes++
	if m.deleteErr != nil {
		return m.deleteErr
	}
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
	res, err := svc.Create(context.Background(), jpegN(t, 300))
	if err != nil {
		t.Fatal(err)
	}
	if res.UploadID != "up-0001" || res.Width != 300 || res.MimeType != "image/jpeg" {
		t.Fatalf("result = %+v", res)
	}
	if res.DeleteAfter != now.Add(application.UploadRetention) {
		t.Fatalf("delete_after = %v", res.DeleteAfter)
	}
	if res.PreviewURLExpiresAt != now.Add(ports.DefaultSignedGetTTL) {
		t.Fatalf("preview_url_expires_at = %v", res.PreviewURLExpiresAt)
	}
	got, ok, err := svc.Assets.GetByID(context.Background(), res.UploadID)
	if err != nil || !ok || !strings.HasPrefix(got.Checksum, application.ChecksumSHA256Prefix) {
		t.Fatalf("repo: %+v ok=%v err=%v", got, ok, err)
	}
}

func TestUploadService_PrepareErrors(t *testing.T) {
	t.Parallel()
	svc := &application.UploadService{
		Assets: &memAssetRepo{},
		Store:  &memBlobStore{},
		IDs:    testutil.NewSeqGenerator("up"),
		Clock:  testutil.NewFakeClock(time.Now().UTC()),
	}
	webp := make([]byte, 12)
	copy(webp[0:4], "RIFF")
	copy(webp[8:12], "WEBP")
	cases := []struct {
		name string
		raw  []byte
		code string
	}{
		{"webp", webp, application.UploadErrUnsupportedMedia},
		{"garbage", []byte("nope.jpg"), application.UploadErrUnsupportedMedia},
		{"corrupt", []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F'}, application.UploadErrInvalidImage},
		{"dims", jpegN(t, 100), application.UploadErrDimensionsOutOfRange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Create(context.Background(), tc.raw)
			var ue *application.UploadError
			if err == nil || !errors.As(err, &ue) || ue.Code != tc.code {
				t.Fatalf("err=%v want %s", err, tc.code)
			}
		})
	}
}

func TestUploadService_Oversize(t *testing.T) {
	t.Parallel()
	svc := &application.UploadService{
		Assets: &memAssetRepo{},
		Store:  &memBlobStore{},
		IDs:    testutil.NewSeqGenerator("up"),
		Clock:  testutil.NewFakeClock(time.Now().UTC()),
	}
	raw := make([]byte, generation.MaxPhotoBytes+1)
	_, err := svc.Create(context.Background(), raw)
	var ue *application.UploadError
	if err == nil || !errors.As(err, &ue) || ue.Code != application.UploadErrPayloadTooLarge {
		t.Fatalf("err=%v", err)
	}
}

func TestUploadService_InsertFailure_DeletesBlob(t *testing.T) {
	t.Parallel()
	store := &memBlobStore{}
	repo := &memAssetRepo{insertErr: fmt.Errorf("db down")}
	svc := &application.UploadService{
		Assets: repo,
		Store:  store,
		IDs:    testutil.NewSeqGenerator("up"),
		Clock:  testutil.NewFakeClock(time.Now().UTC()),
	}
	_, err := svc.Create(context.Background(), jpegN(t, 300))
	if err == nil {
		t.Fatal("want error")
	}
	if store.deletes != 1 || len(store.objects) != 0 {
		t.Fatalf("deletes=%d objects=%d", store.deletes, len(store.objects))
	}
}

func TestUploadService_InsertFailure_DeleteFails(t *testing.T) {
	t.Parallel()
	store := &memBlobStore{deleteErr: fmt.Errorf("delete boom")}
	repo := &memAssetRepo{insertErr: fmt.Errorf("db down")}
	var cleanupOps []string
	svc := &application.UploadService{
		Assets: repo,
		Store:  store,
		IDs:    testutil.NewSeqGenerator("up"),
		Clock:  testutil.NewFakeClock(time.Now().UTC()),
		OnCleanupError: func(op, assetID, storageKey string, err error) {
			cleanupOps = append(cleanupOps, op)
		},
	}
	_, err := svc.Create(context.Background(), jpegN(t, 300))
	if err == nil || !strings.Contains(err.Error(), "blob delete failed") {
		t.Fatalf("err=%v", err)
	}
	if len(store.objects) != 1 {
		t.Fatalf("orphan blob expected when Delete fails after Insert fail, objects=%d", len(store.objects))
	}
	if len(cleanupOps) != 1 || cleanupOps[0] != "delete" {
		t.Fatalf("cleanupOps=%v", cleanupOps)
	}
}

func TestUploadService_SignFailure_CleansUp(t *testing.T) {
	t.Parallel()
	store := &memBlobStore{signErr: fmt.Errorf("sign boom")}
	repo := &memAssetRepo{}
	svc := &application.UploadService{
		Assets: repo,
		Store:  store,
		IDs:    testutil.NewSeqGenerator("up"),
		Clock:  testutil.NewFakeClock(time.Now().UTC()),
	}
	_, err := svc.Create(context.Background(), jpegN(t, 300))
	if err == nil {
		t.Fatal("want error")
	}
	if store.deletes != 1 || len(store.objects) != 0 {
		t.Fatalf("blob cleanup: deletes=%d objects=%d", store.deletes, len(store.objects))
	}
	_, ok, _ := repo.GetByID(context.Background(), "up-0001")
	if ok {
		t.Fatal("row must be soft-deleted after sign failure")
	}
}

func TestUploadService_SignFailure_DeleteFails_AfterSoftDelete(t *testing.T) {
	t.Parallel()
	store := &memBlobStore{signErr: fmt.Errorf("sign boom"), deleteErr: fmt.Errorf("delete boom")}
	repo := &memAssetRepo{}
	var cleanupOps []string
	svc := &application.UploadService{
		Assets: repo,
		Store:  store,
		IDs:    testutil.NewSeqGenerator("up"),
		Clock:  testutil.NewFakeClock(time.Now().UTC()),
		OnCleanupError: func(op, assetID, storageKey string, err error) {
			cleanupOps = append(cleanupOps, op)
			if assetID == "" || storageKey == "" || err == nil {
				t.Fatalf("cleanup callback incomplete: op=%s id=%s key=%s err=%v", op, assetID, storageKey, err)
			}
		},
	}
	_, err := svc.Create(context.Background(), jpegN(t, 300))
	if err == nil || !strings.Contains(err.Error(), "blob delete failed") {
		t.Fatalf("err=%v", err)
	}
	if store.deletes != 1 {
		t.Fatalf("Delete must be attempted after SoftDelete, deletes=%d", store.deletes)
	}
	if len(store.objects) != 1 {
		t.Fatalf("blob must remain when Delete fails, objects=%d", len(store.objects))
	}
	_, ok, _ := repo.GetByID(context.Background(), "up-0001")
	if ok {
		t.Fatal("row must be soft-deleted (hidden) even when blob Delete fails")
	}
	if len(cleanupOps) != 1 || cleanupOps[0] != "delete" {
		t.Fatalf("cleanupOps=%v", cleanupOps)
	}
}

func TestUploadService_SignFailure_SoftDeleteFails_KeepsConsistent(t *testing.T) {
	t.Parallel()
	store := &memBlobStore{signErr: fmt.Errorf("sign boom")}
	repo := &memAssetRepo{softDeleteErr: fmt.Errorf("soft-delete boom")}
	var cleanupOps []string
	svc := &application.UploadService{
		Assets: repo,
		Store:  store,
		IDs:    testutil.NewSeqGenerator("up"),
		Clock:  testutil.NewFakeClock(time.Now().UTC()),
		OnCleanupError: func(op, assetID, storageKey string, err error) {
			cleanupOps = append(cleanupOps, op)
			if assetID == "" || storageKey == "" || err == nil {
				t.Fatalf("cleanup callback incomplete: op=%s id=%s key=%s err=%v", op, assetID, storageKey, err)
			}
		},
	}
	_, err := svc.Create(context.Background(), jpegN(t, 300))
	if err == nil || !strings.Contains(err.Error(), "soft-delete failed") {
		t.Fatalf("err=%v", err)
	}
	if store.deletes != 0 {
		t.Fatalf("must not Delete blob when SoftDelete fails (deletes=%d)", store.deletes)
	}
	if len(store.objects) != 1 {
		t.Fatalf("blob must remain when SoftDelete fails, objects=%d", len(store.objects))
	}
	got, ok, _ := repo.GetByID(context.Background(), "up-0001")
	if !ok || got.DeletedAt != nil {
		t.Fatalf("row must stay active when SoftDelete fails: ok=%v got=%+v", ok, got)
	}
	if len(cleanupOps) != 1 || cleanupOps[0] != "soft_delete" {
		t.Fatalf("cleanupOps=%v", cleanupOps)
	}
}

func TestUploadService_NilDeps(t *testing.T) {
	t.Parallel()
	_, err := (*application.UploadService)(nil).Create(context.Background(), jpegN(t, 300))
	var ue *application.UploadError
	if err == nil || !errors.As(err, &ue) || ue.Code != application.UploadErrUnavailable {
		t.Fatalf("err=%v", err)
	}
}

func TestUploadService_ConcurrencyLimit(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	store := &blockingBlobStore{
		inner:   &memBlobStore{},
		started: started,
		release: release,
	}
	svc := &application.UploadService{
		Assets:      &memAssetRepo{},
		Store:       store,
		IDs:         testutil.NewSeqGenerator("up"),
		Clock:       testutil.NewFakeClock(time.Now().UTC()),
		MaxInFlight: 1,
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := svc.Create(context.Background(), jpegN(t, 300))
		errCh <- err
	}()
	<-started
	_, err := svc.Create(context.Background(), jpegN(t, 300))
	var ue *application.UploadError
	if err == nil || !errors.As(err, &ue) || ue.Code != application.UploadErrTooManyRequests {
		t.Fatalf("second create err=%v", err)
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatalf("first create: %v", err)
	}
}

type blockingBlobStore struct {
	inner   *memBlobStore
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingBlobStore) Put(ctx context.Context, key string, r io.Reader, ct string) error {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return b.inner.Put(ctx, key, r, ct)
}
func (b *blockingBlobStore) Head(ctx context.Context, key string) error {
	return b.inner.Head(ctx, key)
}
func (b *blockingBlobStore) SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return b.inner.SignedGetURL(ctx, key, ttl)
}
func (b *blockingBlobStore) Delete(ctx context.Context, key string) error {
	return b.inner.Delete(ctx, key)
}

var _ ports.AssetStore = (*blockingBlobStore)(nil)
