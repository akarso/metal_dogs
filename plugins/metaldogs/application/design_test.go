package application_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func sha256Sum(b []byte) string {
	sum := sha256.Sum256(b)
	return application.ChecksumSHA256Prefix + hex.EncodeToString(sum[:])
}

// Minimal JPEG magic so imagemime.Sniff accepts clone copies.
var jpegStub = []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46}

type designMemRepo struct {
	mu   sync.Mutex
	byID map[string]domain.Design
}

func (m *designMemRepo) Insert(_ context.Context, d domain.Design) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byID == nil {
		m.byID = map[string]domain.Design{}
	}
	cp := d
	cp.OwnerTokenHash = append([]byte(nil), d.OwnerTokenHash...)
	cp.Texts = copyTexts(d.Texts)
	m.byID[d.ID] = cp
	return nil
}

func (m *designMemRepo) GetByID(_ context.Context, id string) (domain.Design, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.byID[id]
	if !ok {
		return domain.Design{}, false, nil
	}
	cp := d
	cp.OwnerTokenHash = append([]byte(nil), d.OwnerTokenHash...)
	cp.Texts = copyTexts(d.Texts)
	return cp, true, nil
}

func (m *designMemRepo) UpdateEditable(_ context.Context, d domain.Design, expectedSource string, expectedUpdatedAt time.Time, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.byID[d.ID]
	if !ok || cur.SourceAssetID != expectedSource || !cur.UpdatedAt.Equal(expectedUpdatedAt) || !(domain.Design{Status: cur.Status}).EditableForPatch() {
		return ports.ErrDesignConflict
	}
	cur.Texts = copyTexts(d.Texts)
	cur.SourceAssetID = d.SourceAssetID
	cur.Status = d.Status
	cur.UpdatedAt = d.UpdatedAt
	cur.ExpiresAt = d.ExpiresAt
	m.byID[d.ID] = cur
	return nil
}

func (m *designMemRepo) CreateBound(ctx context.Context, d domain.Design, _ time.Time) error {
	return m.Insert(ctx, d)
}

func (m *designMemRepo) ReplaceSource(ctx context.Context, d domain.Design, expectedOld string, expectedUpdatedAt time.Time, deleteAfter time.Time) (string, error) {
	return "", m.UpdateEditable(ctx, d, expectedOld, expectedUpdatedAt, deleteAfter)
}

func copyTexts(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

type designAssetRepo struct {
	mu   sync.Mutex
	byID map[string]domain.Asset
}

func (m *designAssetRepo) Insert(_ context.Context, a domain.Asset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byID == nil {
		m.byID = map[string]domain.Asset{}
	}
	m.byID[a.ID] = a
	return nil
}
func (m *designAssetRepo) GetByID(_ context.Context, id string) (domain.Asset, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[id]
	if !ok || a.DeletedAt != nil {
		return domain.Asset{}, false, nil
	}
	return a, true, nil
}
func (m *designAssetRepo) SoftDelete(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[id]
	if !ok {
		return nil
	}
	if a.DeletedAt == nil {
		a.DeletedAt = &at
		m.byID[id] = a
	}
	return nil
}
func (m *designAssetRepo) BindOwnerRef(_ context.Context, assetID, ownerRef string, deleteAfter time.Time) error {
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
func (m *designAssetRepo) ClearOwnerRef(_ context.Context, assetID, ownerRef string) error {
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
func (m *designAssetRepo) ClearStorageKey(_ context.Context, assetID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[assetID]
	if !ok || a.DeletedAt == nil {
		return fmt.Errorf("clear storage_key: not soft-deleted or missing")
	}
	a.StorageKey = ""
	m.byID[assetID] = a
	return nil
}

type designBlobStore struct {
	objects map[string][]byte
}

func (m *designBlobStore) Put(_ context.Context, key string, r io.Reader, _ string) error {
	b, _ := io.ReadAll(r)
	if m.objects == nil {
		m.objects = map[string][]byte{}
	}
	m.objects[key] = b
	return nil
}
func (m *designBlobStore) Read(_ context.Context, key string) ([]byte, error) {
	b, ok := m.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}
func (m *designBlobStore) Head(_ context.Context, key string) error {
	if _, ok := m.objects[key]; !ok {
		return fmt.Errorf("missing")
	}
	return nil
}
func (m *designBlobStore) SignedGetURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://signed.example/" + key, nil
}
func (m *designBlobStore) Delete(_ context.Context, key string) error {
	if _, ok := m.objects[key]; !ok {
		return os.ErrNotExist
	}
	delete(m.objects, key)
	return nil
}

type designTplStore struct {
	rows map[string]domain.Template
}

func (m *designTplStore) InsertDraft(context.Context, domain.Template) error { return nil }
func (m *designTplStore) LatestBySlug(context.Context, string) (domain.Template, bool, error) {
	return domain.Template{}, false, nil
}
func (m *designTplStore) GetBySlugVersion(context.Context, string, int) (domain.Template, bool, error) {
	return domain.Template{}, false, nil
}
func (m *designTplStore) LatestDraftBySlug(context.Context, string) (domain.Template, bool, error) {
	return domain.Template{}, false, nil
}
func (m *designTplStore) DraftBySlugHash(context.Context, string, string) (domain.Template, bool, error) {
	return domain.Template{}, false, nil
}
func (m *designTplStore) PublishedBySlug(context.Context, string) (domain.Template, bool, error) {
	return domain.Template{}, false, nil
}
func (m *designTplStore) SetStatus(context.Context, string, string) error { return nil }
func (m *designTplStore) PublishExclusive(context.Context, string, string) (domain.Template, error) {
	return domain.Template{}, nil
}
func (m *designTplStore) RetireExclusive(context.Context, string) (domain.Template, error) {
	return domain.Template{}, nil
}
func (m *designTplStore) ListPublished(context.Context, string) ([]ports.PublishedTemplate, error) {
	return nil, nil
}
func (m *designTplStore) GetPublishedByID(_ context.Context, id string) (ports.PublishedTemplate, bool, error) {
	r, ok := m.rows[id]
	if !ok || r.Status != domain.TemplatePublished {
		return ports.PublishedTemplate{}, false, nil
	}
	return ports.PublishedFromDomain(r), true, nil
}

func (m *designTplStore) GetPublicByID(_ context.Context, id string) (ports.PublishedTemplate, bool, error) {
	r, ok := m.rows[id]
	if !ok {
		return ports.PublishedTemplate{}, false, nil
	}
	return ports.PublishedFromDomain(r), true, nil
}

func (m *designTplStore) GetByID(_ context.Context, id string) (domain.Template, bool, error) {
	r, ok := m.rows[id]
	if !ok {
		return domain.Template{}, false, nil
	}
	return r, true, nil
}

// bindingDesignRepo wraps designMemRepo + asset bind for CreateBound/ReplaceSource.
type bindingDesignRepo struct {
	designs *designMemRepo
	assets  *designAssetRepo
}

func (m *bindingDesignRepo) Insert(ctx context.Context, d domain.Design) error {
	return m.designs.Insert(ctx, d)
}
func (m *bindingDesignRepo) GetByID(ctx context.Context, id string) (domain.Design, bool, error) {
	return m.designs.GetByID(ctx, id)
}
func (m *bindingDesignRepo) UpdateEditable(ctx context.Context, d domain.Design, expectedSource string, expectedUpdatedAt time.Time, deleteAfter time.Time) error {
	if err := m.designs.UpdateEditable(ctx, d, expectedSource, expectedUpdatedAt, deleteAfter); err != nil {
		return err
	}
	m.assets.mu.Lock()
	defer m.assets.mu.Unlock()
	if a, ok := m.assets.byID[d.SourceAssetID]; ok && a.DeletedAt == nil {
		a.DeleteAfter = deleteAfter
		m.assets.byID[d.SourceAssetID] = a
	}
	return nil
}
func (m *bindingDesignRepo) CreateBound(ctx context.Context, d domain.Design, deleteAfter time.Time) error {
	if err := m.designs.Insert(ctx, d); err != nil {
		return err
	}
	return m.assets.BindOwnerRef(ctx, d.SourceAssetID, d.ID, deleteAfter)
}
func (m *bindingDesignRepo) ReplaceSource(ctx context.Context, d domain.Design, oldAssetID string, expectedUpdatedAt time.Time, deleteAfter time.Time) (string, error) {
	oldKey := ""
	alreadyDeleted := false
	if oldAssetID != "" && oldAssetID != d.SourceAssetID {
		m.assets.mu.Lock()
		if a, ok := m.assets.byID[oldAssetID]; ok {
			oldKey = a.StorageKey
			alreadyDeleted = a.DeletedAt != nil
		}
		m.assets.mu.Unlock()
		_ = m.assets.SoftDelete(ctx, oldAssetID, d.UpdatedAt) // idempotent
	}
	undoSoftDelete := func() {
		if alreadyDeleted || oldAssetID == "" || oldAssetID == d.SourceAssetID {
			return
		}
		m.assets.mu.Lock()
		if a, ok := m.assets.byID[oldAssetID]; ok {
			a.DeletedAt = nil
			m.assets.byID[oldAssetID] = a
		}
		m.assets.mu.Unlock()
	}
	if err := m.assets.BindOwnerRef(ctx, d.SourceAssetID, d.ID, deleteAfter); err != nil {
		undoSoftDelete()
		return "", err
	}
	if err := m.designs.UpdateEditable(ctx, d, oldAssetID, expectedUpdatedAt, deleteAfter); err != nil {
		_ = m.assets.ClearOwnerRef(ctx, d.SourceAssetID, d.ID)
		undoSoftDelete()
		return "", err
	}
	return oldKey, nil
}

func publishedTpl(id string) domain.Template {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	return domain.Template{
		ID: id, Slug: "head-front", Status: domain.TemplatePublished, Genre: "black_metal",
		StyleCategory: "head_portrait", Version: 2, ContentHash: "h",
		TextFields: domain.TextFields{
			Editable: []domain.EditableText{{Key: "dog_name", MaxLength: 12, AllowedCharset: "latin_polish"}},
			Fixed:    []domain.FixedText{{Key: "tagline", Value: "METAL"}},
		},
		PublishedAt: &now,
	}
}

func TestDesignService_CreateGetPatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111111"
	uploadID := "22222222-2222-4222-8222-222222222222"
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: "private/blobs/metaldogs/source/2026/11/x.jpg",
			MimeType: "image/jpeg", SizeBytes: 10, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: designs, assets: assets},
		Assets:    assets,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{"private/blobs/metaldogs/source/2026/11/x.jpg": {1}}},
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     testutil.NewFakeClock(now),
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.OwnerToken == "" || created.Status != domain.DesignDraft || created.Texts["tagline"] != "METAL" {
		t.Fatalf("%+v", created)
	}
	if created.ExpiresAt != now.Add(domain.DesignRetentionUnordered) {
		t.Fatalf("expires %v", created.ExpiresAt)
	}
	got, err := svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err != nil || got.OwnerToken != "" || got.UploadID != uploadID {
		t.Fatalf("%+v err=%v", got, err)
	}
	_, err = svc.Get(context.Background(), created.ID, "wrong")
	var de *application.DesignError
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrNotFound {
		t.Fatalf("err=%v", err)
	}
	patched, err := svc.Patch(context.Background(), created.ID, created.OwnerToken, application.PatchInput{
		Texts: map[string]string{"dog_name": "Azor"},
	})
	if err != nil || patched.Texts["dog_name"] != "Azor" {
		t.Fatalf("%+v err=%v", patched, err)
	}
}

func TestDesignService_Create_UnpublishedTemplate(t *testing.T) {
	t.Parallel()
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: &designMemRepo{}, assets: &designAssetRepo{}},
		Assets:    &designAssetRepo{},
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{}}},
		Store:     &designBlobStore{},
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     testutil.NewFakeClock(time.Now().UTC()),
	}
	_, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: "11111111-1111-4111-8111-111111111111",
		UploadID:   "22222222-2222-4222-8222-222222222222",
		Texts:      map[string]string{"dog_name": "Burek"},
	})
	var de *application.DesignError
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrTemplateUnavailable {
		t.Fatalf("err=%v", err)
	}
}

type signFailBlobStore struct {
	designBlobStore
	fail bool
}

func (m *signFailBlobStore) SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if m.fail {
		return "", errors.New("sign boom")
	}
	return m.designBlobStore.SignedGetURL(ctx, key, ttl)
}

func TestDesignService_Create_SignFailureStillReturnsOwnerToken(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111111"
	uploadID := "22222222-2222-4222-8222-222222222222"
	key := "private/blobs/metaldogs/source/2026/11/x.jpg"
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: key,
			MimeType: "image/jpeg", SizeBytes: 10, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	store := &signFailBlobStore{designBlobStore: designBlobStore{objects: map[string][]byte{key: {1}}}, fail: true}
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: &designMemRepo{}, assets: assets},
		Assets:    assets,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     store,
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     testutil.NewFakeClock(now),
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.OwnerToken == "" {
		t.Fatal("owner_token must be returned after commit even when sign fails")
	}
	if created.SourcePreviewURL != "" || !created.SourcePreviewURLExpiresAt.IsZero() {
		t.Fatalf("preview must soft-fail empty: %+v", created)
	}
	if created.UploadID != uploadID {
		t.Fatalf("sign failure must keep upload_id, got %q", created.UploadID)
	}
	bound, ok, _ := assets.GetByID(context.Background(), uploadID)
	if !ok || bound.OwnerRef == "" {
		t.Fatal("upload must stay bound")
	}
	store.fail = false
	got, err := svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err != nil || got.SourcePreviewURL == "" {
		t.Fatalf("GET should sign again: %+v err=%v", got, err)
	}
}

func TestDesignService_Get_SignFailureKeepsUploadID(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111115"
	uploadID := "22222222-2222-4222-8222-222222222228"
	key := "private/blobs/metaldogs/source/2026/11/live.jpg"
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: key,
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	store := &signFailBlobStore{designBlobStore: designBlobStore{objects: map[string][]byte{key: {1}}}, fail: false}
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: &designMemRepo{}, assets: assets},
		Assets:    assets,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     store,
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     testutil.NewFakeClock(now),
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	store.fail = true
	got, err := svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.UploadID != uploadID {
		t.Fatalf("live source + sign failure must keep upload_id, got %q", got.UploadID)
	}
	if got.SourcePreviewURL != "" {
		t.Fatalf("preview must be empty on sign failure: %q", got.SourcePreviewURL)
	}
}

func TestDesignService_Get_SweptSourceOmitsUploadID(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111116"
	uploadID := "22222222-2222-4222-8222-222222222229"
	key := "private/blobs/metaldogs/source/2026/11/gone.jpg"
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: key,
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: &designMemRepo{}, assets: assets},
		Assets:    assets,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{key: {1}}},
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     testutil.NewFakeClock(now),
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := assets.SoftDelete(context.Background(), uploadID, now); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.UploadID != "" {
		t.Fatalf("swept source must omit upload_id, got %q", got.UploadID)
	}
	if got.SourcePreviewURL != "" {
		t.Fatalf("swept source preview must be empty: %q", got.SourcePreviewURL)
	}
}

type errAssetRepo struct {
	designAssetRepo
	getErr error
	failID string // when set with getErr, only this id fails; empty → all GetByID fail
}

func (m *errAssetRepo) GetByID(ctx context.Context, id string) (domain.Asset, bool, error) {
	if m.getErr != nil && (m.failID == "" || id == m.failID) {
		return domain.Asset{}, false, m.getErr
	}
	return m.designAssetRepo.GetByID(ctx, id)
}

func TestDesignService_Create_PostCommitAssetLookupStillReturnsToken(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111118"
	uploadID := "22222222-2222-4222-8222-22222222222b"
	key := "private/blobs/metaldogs/source/2026/11/create-err.jpg"
	inner := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: key,
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	// Create pre-check GetByID succeeds once; post-commit toView GetByID fails.
	counting := &countdownAssetRepo{inner: inner, failAfter: 1}
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: &designMemRepo{}, assets: inner},
		Assets:    counting,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{key: {1}}},
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     testutil.NewFakeClock(now),
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.OwnerToken == "" {
		t.Fatal("owner_token must be returned after CreateBound even when toView asset lookup fails")
	}
	if created.UploadID != uploadID {
		t.Fatalf("upload_id=%q", created.UploadID)
	}
	if created.SourcePreviewURL != "" {
		t.Fatalf("preview must be empty on soft-fail: %q", created.SourcePreviewURL)
	}
	bound, ok, _ := inner.GetByID(context.Background(), uploadID)
	if !ok || bound.OwnerRef == "" {
		t.Fatal("upload must stay bound")
	}
}

// countdownAssetRepo succeeds for the first n GetByID calls, then returns an error.
type countdownAssetRepo struct {
	inner     *designAssetRepo
	failAfter int
	n         int
}

func (m *countdownAssetRepo) Insert(ctx context.Context, a domain.Asset) error {
	return m.inner.Insert(ctx, a)
}
func (m *countdownAssetRepo) GetByID(ctx context.Context, id string) (domain.Asset, bool, error) {
	m.n++
	if m.n > m.failAfter {
		return domain.Asset{}, false, errors.New("db down after commit")
	}
	return m.inner.GetByID(ctx, id)
}
func (m *countdownAssetRepo) SoftDelete(ctx context.Context, id string, at time.Time) error {
	return m.inner.SoftDelete(ctx, id, at)
}
func (m *countdownAssetRepo) BindOwnerRef(ctx context.Context, assetID, ownerRef string, deleteAfter time.Time) error {
	return m.inner.BindOwnerRef(ctx, assetID, ownerRef, deleteAfter)
}
func (m *countdownAssetRepo) ClearOwnerRef(ctx context.Context, assetID, ownerRef string) error {
	return m.inner.ClearOwnerRef(ctx, assetID, ownerRef)
}
func (m *countdownAssetRepo) ClearStorageKey(ctx context.Context, assetID string) error {
	return m.inner.ClearStorageKey(ctx, assetID)
}

func TestDesignService_Get_AssetLookupError(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111117"
	uploadID := "22222222-2222-4222-8222-22222222222a"
	key := "private/blobs/metaldogs/source/2026/11/err.jpg"
	inner := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: key,
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: &designMemRepo{}, assets: inner},
		Assets:    inner,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{key: {1}}},
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     testutil.NewFakeClock(now),
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Assets = &errAssetRepo{designAssetRepo: *inner, getErr: errors.New("db down")}
	_, err = svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err == nil {
		t.Fatal("want asset lookup error")
	}
	var de *application.DesignError
	if errors.As(err, &de) {
		t.Fatalf("lookup I/O must not be a DesignError, got %v", de)
	}
}

func TestDesignService_Patch_PinnedRetiredTemplate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111111"
	uploadID := "22222222-2222-4222-8222-222222222222"
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: "k",
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	tpl := publishedTpl(tplID)
	store := &designTplStore{rows: map[string]domain.Template{tplID: tpl}}
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: &designMemRepo{}, assets: assets},
		Assets:    assets,
		Templates: &application.TemplateCatalog{Store: store},
		Store:     &designBlobStore{objects: map[string][]byte{"k": {1}}},
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     testutil.NewFakeClock(now),
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tpl.Status = domain.TemplateRetired
	store.rows[tplID] = tpl
	patched, err := svc.Patch(context.Background(), created.ID, created.OwnerToken, application.PatchInput{
		Texts: map[string]string{"dog_name": "Azor"},
	})
	if err != nil || patched.Texts["dog_name"] != "Azor" {
		t.Fatalf("pinned retired template must still validate texts: %+v err=%v", patched, err)
	}
}

func TestDesignService_Get_DoesNotSlideExpiresAt(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111112"
	uploadID := "22222222-2222-4222-8222-222222222223"
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: "k",
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	clock := testutil.NewFakeClock(now)
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: designs, assets: assets},
		Assets:    assets,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{"k": {1}}},
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     clock,
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantExp := created.ExpiresAt
	clock.Advance(24 * time.Hour)
	got, err := svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.Equal(wantExp) {
		t.Fatalf("GET must not slide expires_at: got %v want %v", got.ExpiresAt, wantExp)
	}
}

func TestDesignService_ReplaceSource_AlreadySweptOld_NoHardCleanupLog(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111114"
	uploadID := "22222222-2222-4222-8222-222222222226"
	upload2 := "22222222-2222-4222-8222-222222222227"
	key1 := "private/blobs/metaldogs/source/2026/11/swept.jpg"
	key2 := "private/blobs/metaldogs/source/2026/11/new2.jpg"
	deletedAt := now.Add(-time.Hour)
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: key1,
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour), DeletedAt: &deletedAt, OwnerRef: "will-set",
		},
		upload2: {
			ID: upload2, Type: domain.AssetTypeSource, StorageKey: key2,
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:cd",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	// Design still points at swept upload; blob already gone from store.
	designs := &designMemRepo{byID: map[string]domain.Design{}}
	hashTok, hash, err := application.MintOwnerToken()
	if err != nil {
		t.Fatal(err)
	}
	designID := "des-0001"
	assets.byID[uploadID] = func() domain.Asset {
		a := assets.byID[uploadID]
		a.OwnerRef = designID
		return a
	}()
	_ = designs.Insert(context.Background(), domain.Design{
		ID: designID, TemplateID: tplID, TemplateVersion: 2, Status: domain.DesignDraft,
		Texts:         map[string]string{"dog_name": "Burek", "tagline": "METAL"},
		SourceAssetID: uploadID, OwnerTokenHash: hash,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(domain.DesignRetentionUnordered),
	})
	store := &designBlobStore{objects: map[string][]byte{key2: {2}}} // key1 already absent
	var cleanupLogs []string
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: designs, assets: assets},
		Assets:    assets,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     store,
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     testutil.NewFakeClock(now),
		OnCleanupError: func(op, assetID, storageKey string, err error) {
			cleanupLogs = append(cleanupLogs, op+":"+err.Error())
		},
	}
	patched, err := svc.Patch(context.Background(), designID, hashTok, application.PatchInput{
		UploadID: &upload2,
	})
	if err != nil || patched.UploadID != upload2 {
		t.Fatalf("%+v err=%v", patched, err)
	}
	if len(cleanupLogs) != 0 {
		t.Fatalf("missing object must not hard-fail cleanup: %v", cleanupLogs)
	}
	gotOld, ok, _ := assets.GetByID(context.Background(), uploadID)
	if ok {
		t.Fatalf("old must stay soft-deleted, got %+v", gotOld)
	}
	assets.mu.Lock()
	old := assets.byID[uploadID]
	assets.mu.Unlock()
	if old.DeletedAt == nil {
		t.Fatal("must not clear DeletedAt on already-swept row")
	}
	if old.StorageKey != "" {
		t.Fatalf("storage_key should be cleared after absent delete, got %q", old.StorageKey)
	}
	bound, ok, _ := assets.GetByID(context.Background(), upload2)
	if !ok || bound.OwnerRef != designID {
		t.Fatalf("new upload bound=%+v ok=%v", bound, ok)
	}
}

func TestDesignService_ReplaceSource_DeletesOldBlob(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111113"
	uploadID := "22222222-2222-4222-8222-222222222224"
	upload2 := "22222222-2222-4222-8222-222222222225"
	key1 := "private/blobs/metaldogs/source/2026/11/old.jpg"
	key2 := "private/blobs/metaldogs/source/2026/11/new.jpg"
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: key1,
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
		upload2: {
			ID: upload2, Type: domain.AssetTypeSource, StorageKey: key2,
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:cd",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	store := &designBlobStore{objects: map[string][]byte{key1: {1}, key2: {2}}}
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: &designMemRepo{}, assets: assets},
		Assets:    assets,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     store,
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     testutil.NewFakeClock(now),
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	patched, err := svc.Patch(context.Background(), created.ID, created.OwnerToken, application.PatchInput{
		UploadID: &upload2,
	})
	if err != nil || patched.UploadID != upload2 {
		t.Fatalf("%+v err=%v", patched, err)
	}
	if _, ok := store.objects[key1]; ok {
		t.Fatal("old blob must be deleted after replace")
	}
	if _, ok := store.objects[key2]; !ok {
		t.Fatal("new blob must remain")
	}
	if _, ok, _ := assets.GetByID(context.Background(), uploadID); ok {
		t.Fatal("old asset must be soft-deleted")
	}
	assets.mu.Lock()
	old := assets.byID[uploadID]
	assets.mu.Unlock()
	if old.StorageKey != "" {
		t.Fatalf("storage_key must be cleared after successful delete, got %q", old.StorageKey)
	}
}

func TestDesignService_Get_ExpiredNotFound(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111111"
	uploadID := "22222222-2222-4222-8222-222222222222"
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: "k",
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	clock := testutil.NewFakeClock(now)
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: &designMemRepo{}, assets: assets},
		Assets:    assets,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{"k": {1}}},
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     clock,
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(domain.DesignRetentionUnordered + time.Second)
	_, err = svc.Get(context.Background(), created.ID, created.OwnerToken)
	var de *application.DesignError
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrNotFound {
		t.Fatalf("err=%v", err)
	}
}

// designJobRepo is a GetByID-only stub for preview URL tests.
type designJobRepo struct {
	byID map[string]domain.GenerationJob
}

func (r *designJobRepo) Insert(context.Context, domain.GenerationJob) error { return nil }
func (r *designJobRepo) GetByID(_ context.Context, id string) (domain.GenerationJob, bool, error) {
	j, ok := r.byID[id]
	return j, ok, nil
}
func (r *designJobRepo) GetByIdempotency(context.Context, string, string) (domain.GenerationJob, bool, error) {
	return domain.GenerationJob{}, false, nil
}
func (r *designJobRepo) GetActiveByDesign(context.Context, string) (domain.GenerationJob, bool, error) {
	return domain.GenerationJob{}, false, nil
}
func (r *designJobRepo) BeginGenerating(context.Context, domain.GenerationJob, domain.Design, time.Time, int, string) error {
	return nil
}
func (r *designJobRepo) ClaimForWork(context.Context, string, time.Duration) (domain.GenerationJob, error) {
	return domain.GenerationJob{}, nil
}
func (r *designJobRepo) ClaimLeaseExpired(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}
func (r *designJobRepo) InvalidateClaim(context.Context, string) error              { return nil }
func (r *designJobRepo) Update(context.Context, domain.GenerationJob, string) error { return nil }
func (r *designJobRepo) PersistArtwork(context.Context, domain.GenerationJob, domain.Asset) ([]ports.DeletedBlobRef, error) {
	return nil, nil
}
func (r *designJobRepo) CompleteSuccess(context.Context, domain.GenerationJob, domain.Asset, domain.Design) ([]ports.DeletedBlobRef, error) {
	return nil, nil
}
func (r *designJobRepo) FinalizeSuccess(context.Context, domain.GenerationJob, domain.Design, domain.Asset) ([]ports.DeletedBlobRef, error) {
	return nil, nil
}
func (r *designJobRepo) CompleteFailure(context.Context, domain.GenerationJob, domain.Design) error {
	return nil
}
func (r *designJobRepo) FailActive(context.Context, string, string, string, string, int64, time.Time) error {
	return nil
}

type failKeySignStore struct {
	*designBlobStore
	failKey string
}

func (s *failKeySignStore) SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if key == s.failKey {
		return "", errors.New("sign failed")
	}
	return s.designBlobStore.SignedGetURL(ctx, key, ttl)
}

type errDesignJobRepo struct {
	inner *designJobRepo
	err   error
}

func (r *errDesignJobRepo) Insert(ctx context.Context, j domain.GenerationJob) error {
	return r.inner.Insert(ctx, j)
}
func (r *errDesignJobRepo) GetByID(ctx context.Context, id string) (domain.GenerationJob, bool, error) {
	if r.err != nil {
		return domain.GenerationJob{}, false, r.err
	}
	return r.inner.GetByID(ctx, id)
}
func (r *errDesignJobRepo) GetByIdempotency(ctx context.Context, designID, key string) (domain.GenerationJob, bool, error) {
	return r.inner.GetByIdempotency(ctx, designID, key)
}
func (r *errDesignJobRepo) GetActiveByDesign(ctx context.Context, designID string) (domain.GenerationJob, bool, error) {
	return r.inner.GetActiveByDesign(ctx, designID)
}
func (r *errDesignJobRepo) BeginGenerating(ctx context.Context, j domain.GenerationJob, d domain.Design, at time.Time, expectedAttempts int, expectedLatestJobID string) error {
	return r.inner.BeginGenerating(ctx, j, d, at, expectedAttempts, expectedLatestJobID)
}
func (r *errDesignJobRepo) ClaimForWork(ctx context.Context, id string, lease time.Duration) (domain.GenerationJob, error) {
	return r.inner.ClaimForWork(ctx, id, lease)
}
func (r *errDesignJobRepo) ClaimLeaseExpired(ctx context.Context, id string, lease time.Duration) (bool, error) {
	return r.inner.ClaimLeaseExpired(ctx, id, lease)
}
func (r *errDesignJobRepo) InvalidateClaim(ctx context.Context, id string) error {
	return r.inner.InvalidateClaim(ctx, id)
}
func (r *errDesignJobRepo) Update(ctx context.Context, j domain.GenerationJob, expected string) error {
	return r.inner.Update(ctx, j, expected)
}
func (r *errDesignJobRepo) PersistArtwork(ctx context.Context, j domain.GenerationJob, artwork domain.Asset) ([]ports.DeletedBlobRef, error) {
	return r.inner.PersistArtwork(ctx, j, artwork)
}
func (r *errDesignJobRepo) CompleteSuccess(ctx context.Context, j domain.GenerationJob, mockup domain.Asset, design domain.Design) ([]ports.DeletedBlobRef, error) {
	return r.inner.CompleteSuccess(ctx, j, mockup, design)
}
func (r *errDesignJobRepo) FinalizeSuccess(ctx context.Context, j domain.GenerationJob, design domain.Design, mockup domain.Asset) ([]ports.DeletedBlobRef, error) {
	return r.inner.FinalizeSuccess(ctx, j, design, mockup)
}
func (r *errDesignJobRepo) CompleteFailure(ctx context.Context, j domain.GenerationJob, design domain.Design) error {
	return r.inner.CompleteFailure(ctx, j, design)
}
func (r *errDesignJobRepo) FailActive(ctx context.Context, id, designID, terminalReason, errorCode string, claimToken int64, at time.Time) error {
	return r.inner.FailActive(ctx, id, designID, terminalReason, errorCode, claimToken, at)
}

func TestDesignService_Get_ArtworkAndMockupPreviews(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111111"
	uploadID := "22222222-2222-4222-8222-222222222222"
	artID := "33333333-3333-4333-8333-333333333333"
	mockID := "44444444-4444-4444-8444-444444444444"
	jobID := "55555555-5555-4555-8555-555555555555"
	otherArtID := "66666666-6666-4666-8666-666666666666"
	sourceAsArtID := "77777777-7777-4777-8777-777777777777"
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: "src-k",
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
		artID: {
			ID: artID, Type: domain.AssetTypeArtwork, StorageKey: "art-k",
			MimeType: "image/png", SizeBytes: 1, Width: 64, Height: 64, Checksum: "sha256:art",
			DeleteAfter: now.Add(time.Hour),
		},
		mockID: {
			ID: mockID, Type: domain.AssetTypeMockup, StorageKey: "mock-k",
			MimeType: "image/png", SizeBytes: 1, Width: 80, Height: 100, Checksum: "sha256:mock",
			DeleteAfter: now.Add(time.Hour),
		},
		otherArtID: {
			ID: otherArtID, Type: domain.AssetTypeArtwork, StorageKey: "other-art-k", OwnerRef: "other-design",
			MimeType: "image/png", SizeBytes: 1, Width: 64, Height: 64, Checksum: "sha256:other",
			DeleteAfter: now.Add(time.Hour),
		},
		sourceAsArtID: {
			ID: sourceAsArtID, Type: domain.AssetTypeSource, StorageKey: "src-as-art-k",
			MimeType: "image/jpeg", SizeBytes: 1, Width: 64, Height: 64, Checksum: "sha256:src",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	jobs := &designJobRepo{byID: map[string]domain.GenerationJob{}}
	store := &designBlobStore{objects: map[string][]byte{
		"src-k": {1}, "art-k": {2}, "mock-k": {3}, "other-art-k": {4}, "src-as-art-k": {5},
	}}
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: designs, assets: assets},
		Assets:    assets,
		Jobs:      jobs,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     store,
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     testutil.NewFakeClock(now),
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assets.mu.Lock()
	art := assets.byID[artID]
	art.OwnerRef = created.ID
	assets.byID[artID] = art
	mock := assets.byID[mockID]
	mock.OwnerRef = created.ID
	assets.byID[mockID] = mock
	srcAsArt := assets.byID[sourceAsArtID]
	srcAsArt.OwnerRef = created.ID
	assets.byID[sourceAsArtID] = srcAsArt
	assets.mu.Unlock()

	setPreview := func(status, artRef, mockRef, jobDesignID, jobStatus string) {
		t.Helper()
		d, ok, _ := designs.GetByID(context.Background(), created.ID)
		if !ok {
			t.Fatal("design missing")
		}
		d.Status = status
		d.LatestJobID = jobID
		_ = designs.Insert(context.Background(), d)
		jobs.byID[jobID] = domain.GenerationJob{
			ID: jobID, DesignID: jobDesignID, Status: jobStatus,
			ArtworkAssetID: artRef, MockupAssetID: mockRef,
		}
	}

	setPreview(domain.DesignPreviewReady, artID, mockID, created.ID, domain.JobSucceeded)
	got, err := svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtworkPreviewURL != "https://signed.example/art-k" || got.MockupPreviewURL != "https://signed.example/mock-k" {
		t.Fatalf("urls artwork=%q mockup=%q", got.ArtworkPreviewURL, got.MockupPreviewURL)
	}

	setPreview(domain.DesignPreviewReady, otherArtID, mockID, created.ID, domain.JobSucceeded)
	got, err = svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtworkPreviewURL != "" || !strings.Contains(got.MockupPreviewURL, "mock-k") {
		t.Fatalf("foreign artwork must be omitted: %+v", got)
	}

	setPreview(domain.DesignPreviewReady, sourceAsArtID, mockID, created.ID, domain.JobSucceeded)
	got, err = svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtworkPreviewURL != "" {
		t.Fatalf("source-typed asset must not sign as artwork: %q", got.ArtworkPreviewURL)
	}

	setPreview(domain.DesignPreviewReady, artID, mockID, "other-design", domain.JobSucceeded)
	got, err = svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtworkPreviewURL != "" || got.MockupPreviewURL != "" {
		t.Fatalf("mismatched job design must omit previews: %+v", got)
	}

	setPreview(domain.DesignPreviewReady, artID, mockID, created.ID, domain.JobFailed)
	got, err = svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtworkPreviewURL != "" || got.MockupPreviewURL != "" {
		t.Fatalf("failed job must omit previews: %+v", got)
	}

	setPreview(domain.DesignPreviewReady, artID, mockID, created.ID, domain.JobSucceeded)
	svc.Store = &failKeySignStore{designBlobStore: store, failKey: "art-k"}
	got, err = svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtworkPreviewURL != "" || got.MockupPreviewURL != "https://signed.example/mock-k" {
		t.Fatalf("signer failure: %+v", got)
	}
	svc.Store = store

	// Source GetByID succeeds once; artwork GetByID then fails → 500 path.
	setPreview(domain.DesignPreviewReady, artID, mockID, created.ID, domain.JobSucceeded)
	svc.Assets = &countdownAssetRepo{inner: assets, failAfter: 1}
	_, err = svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err == nil {
		t.Fatal("want artwork lookup error")
	}
	svc.Assets = assets

	// Job GetByID I/O error → 500.
	svc.Jobs = &errDesignJobRepo{inner: jobs, err: errors.New("job db down")}
	_, err = svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err == nil {
		t.Fatal("want job lookup error")
	}
	svc.Jobs = jobs

	setPreview(domain.DesignApproved, artID, mockID, created.ID, domain.JobSucceeded)
	got, err = svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtworkPreviewURL == "" || got.MockupPreviewURL == "" {
		t.Fatalf("approved must sign: %+v", got)
	}

	setPreview(domain.DesignDraft, artID, mockID, created.ID, domain.JobSucceeded)
	draft, err := svc.Get(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if draft.ArtworkPreviewURL != "" || draft.MockupPreviewURL != "" {
		t.Fatalf("draft must omit generation previews: %+v", draft)
	}
}

type versionMemRepo struct {
	mu       sync.Mutex
	byID     map[string]domain.DesignVersion
	byDesign map[string]string
	designs  *designMemRepo
	assets   *designAssetRepo
}

func (r *versionMemRepo) GetByID(_ context.Context, id string) (domain.DesignVersion, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.byID[id]
	if !ok {
		return domain.DesignVersion{}, false, nil
	}
	cp := v
	cp.Texts = copyTexts(v.Texts)
	cp.ParametersJSON = append([]byte(nil), v.ParametersJSON...)
	return cp, true, nil
}

func (r *versionMemRepo) GetByDesignID(_ context.Context, designID string) (domain.DesignVersion, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byDesign[designID]
	if !ok {
		return domain.DesignVersion{}, false, nil
	}
	v := r.byID[id]
	cp := v
	cp.Texts = copyTexts(v.Texts)
	cp.ParametersJSON = append([]byte(nil), v.ParametersJSON...)
	return cp, true, nil
}

func (r *versionMemRepo) ApproveBound(_ context.Context, d domain.Design, v domain.DesignVersion) error {
	if d.ID == "" || v.ID == "" || v.DesignID != d.ID || d.ApprovedVersionID != v.ID {
		return fmt.Errorf("approve id mismatch")
	}
	r.designs.mu.Lock()
	cur, ok := r.designs.byID[d.ID]
	if !ok || cur.Status != domain.DesignPreviewReady {
		r.designs.mu.Unlock()
		return ports.ErrDesignConflict
	}
	r.designs.mu.Unlock()
	if r.assets != nil {
		r.assets.mu.Lock()
		art, aok := r.assets.byID[v.ArtworkAssetID]
		r.assets.mu.Unlock()
		if !aok || art.DeletedAt != nil || art.Type != domain.AssetTypeArtwork ||
			art.OwnerRef != d.ID || art.StorageKey == "" || art.Checksum != v.ArtworkChecksum {
			return ports.ErrDesignConflict
		}
	}
	r.mu.Lock()
	if r.byID == nil {
		r.byID = map[string]domain.DesignVersion{}
		r.byDesign = map[string]string{}
	}
	if _, exists := r.byDesign[v.DesignID]; exists {
		r.mu.Unlock()
		return ports.ErrDesignConflict
	}
	cp := v
	cp.Texts = copyTexts(v.Texts)
	cp.ParametersJSON = append([]byte(nil), v.ParametersJSON...)
	r.byID[v.ID] = cp
	r.byDesign[v.DesignID] = v.ID
	r.mu.Unlock()

	r.designs.mu.Lock()
	defer r.designs.mu.Unlock()
	cur, ok = r.designs.byID[d.ID]
	if !ok || cur.Status != domain.DesignPreviewReady {
		r.mu.Lock()
		delete(r.byID, v.ID)
		delete(r.byDesign, v.DesignID)
		r.mu.Unlock()
		return ports.ErrDesignConflict
	}
	cur.Status = domain.DesignApproved
	cur.ApprovedVersionID = v.ID
	cur.UpdatedAt = d.UpdatedAt
	cur.ExpiresAt = d.ExpiresAt
	r.designs.byID[d.ID] = cur
	if r.assets != nil {
		keep := d.ExpiresAt
		r.assets.mu.Lock()
		for _, id := range []string{d.SourceAssetID, v.ArtworkAssetID} {
			if a, ok := r.assets.byID[id]; ok && a.DeletedAt == nil {
				if a.DeleteAfter.Before(keep) {
					a.DeleteAfter = keep
					r.assets.byID[id] = a
				}
			}
		}
		r.assets.mu.Unlock()
	}
	return nil
}

func TestDesignService_ApproveAndClone(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111111"
	uploadID := "22222222-2222-4222-8222-222222222222"
	artID := "33333333-3333-4333-8333-333333333333"
	jobID := "55555555-5555-4555-8555-555555555555"
	srcKey := "private/blobs/metaldogs/source/2026/11/x.jpg"
	artBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: srcKey,
			MimeType: "image/jpeg", SizeBytes: int64(len(jpegStub)), Width: 300, Height: 300,
			Checksum: sha256Sum(jpegStub), DeleteAfter: now.Add(time.Hour),
		},
		artID: {
			ID: artID, Type: domain.AssetTypeArtwork, StorageKey: "art-k",
			MimeType: "image/png", SizeBytes: int64(len(artBytes)), Width: 64, Height: 64,
			Checksum: sha256Sum(artBytes), DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	jobs := &designJobRepo{byID: map[string]domain.GenerationJob{}}
	versions := &versionMemRepo{designs: designs, assets: assets}
	store := &designBlobStore{objects: map[string][]byte{srcKey: append([]byte(nil), jpegStub...), "art-k": append([]byte(nil), artBytes...)}}
	var enqueued []string
	svc := &application.DesignService{
		Designs:   &bindingDesignRepo{designs: designs, assets: assets},
		Assets:    assets,
		Jobs:      jobs,
		Versions:  versions,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     store,
		IDs:       testutil.NewSeqGenerator("des"),
		Clock:     testutil.NewFakeClock(now),
		EnqueuePrepare: func(_ context.Context, versionID, designID string) error {
			enqueued = append(enqueued, versionID+"|"+designID)
			return nil
		},
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assets.mu.Lock()
	art := assets.byID[artID]
	art.OwnerRef = created.ID
	assets.byID[artID] = art
	assets.mu.Unlock()

	_, err = svc.Approve(context.Background(), created.ID, created.OwnerToken)
	var de *application.DesignError
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrInvalidTransition {
		t.Fatalf("draft approve: %v", err)
	}

	d, _, _ := designs.GetByID(context.Background(), created.ID)
	d.Status = domain.DesignPreviewReady
	d.LatestJobID = jobID
	_ = designs.Insert(context.Background(), d)
	jobs.byID[jobID] = domain.GenerationJob{
		ID: jobID, DesignID: created.ID, Status: domain.JobSucceeded,
		ArtworkAssetID: artID, Provider: "fake", Model: "fake", PromptVersion: "pv1",
		ParametersJSON: []byte(`{"n":1}`),
	}

	res, err := svc.Approve(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if res.Design.Status != domain.DesignApproved || res.Version.ID == "" || res.Version.ArtworkAssetID != artID {
		t.Fatalf("%+v", res)
	}
	if len(enqueued) != 1 || !strings.Contains(enqueued[0], created.ID) {
		t.Fatalf("enqueue=%v", enqueued)
	}
	verID := res.Version.ID

	res2, err := svc.Approve(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Version.ID != verID {
		t.Fatalf("idempotent version %q want %q", res2.Version.ID, verID)
	}
	if len(enqueued) != 2 {
		t.Fatalf("idempotent must re-enqueue prepare: %v", enqueued)
	}

	cloned, err := svc.Clone(context.Background(), created.ID, created.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if cloned.ID == created.ID || cloned.OwnerToken == "" || cloned.OwnerToken == created.OwnerToken {
		t.Fatalf("clone identity: %+v", cloned)
	}
	if cloned.Status != domain.DesignDraft || cloned.Texts["dog_name"] != "Burek" || cloned.UploadID == uploadID {
		t.Fatalf("clone fields: %+v", cloned)
	}
	newAsset, ok, _ := assets.GetByID(context.Background(), cloned.UploadID)
	if !ok || newAsset.StorageKey == srcKey {
		t.Fatalf("clone source: %+v", newAsset)
	}
	if _, ok := store.objects[newAsset.StorageKey]; !ok {
		t.Fatal("clone bytes missing")
	}

	_, err = svc.Clone(context.Background(), created.ID, "wrong-token")
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrNotFound {
		t.Fatalf("wrong token: %v", err)
	}
	_, err = svc.Approve(context.Background(), cloned.ID, cloned.OwnerToken)
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrInvalidTransition {
		t.Fatalf("unapproved clone approve: %v", err)
	}
}

type countdownJobRepo struct {
	inner     *designJobRepo
	failAfter int
	n         int
	mu        sync.Mutex
	errAfter  error // when set, GetByID returns this error after failAfter successes
}

func (r *countdownJobRepo) GetByID(ctx context.Context, id string) (domain.GenerationJob, bool, error) {
	r.mu.Lock()
	r.n++
	n := r.n
	r.mu.Unlock()
	if n > r.failAfter {
		if r.errAfter != nil {
			return domain.GenerationJob{}, false, r.errAfter
		}
		return domain.GenerationJob{}, false, nil
	}
	return r.inner.GetByID(ctx, id)
}
func (r *countdownJobRepo) Insert(ctx context.Context, j domain.GenerationJob) error {
	return r.inner.Insert(ctx, j)
}
func (r *countdownJobRepo) GetByIdempotency(ctx context.Context, designID, key string) (domain.GenerationJob, bool, error) {
	return r.inner.GetByIdempotency(ctx, designID, key)
}
func (r *countdownJobRepo) GetActiveByDesign(ctx context.Context, designID string) (domain.GenerationJob, bool, error) {
	return r.inner.GetActiveByDesign(ctx, designID)
}
func (r *countdownJobRepo) BeginGenerating(ctx context.Context, j domain.GenerationJob, d domain.Design, at time.Time, expectedAttempts int, expectedLatestJobID string) error {
	return r.inner.BeginGenerating(ctx, j, d, at, expectedAttempts, expectedLatestJobID)
}
func (r *countdownJobRepo) ClaimForWork(ctx context.Context, id string, lease time.Duration) (domain.GenerationJob, error) {
	return r.inner.ClaimForWork(ctx, id, lease)
}
func (r *countdownJobRepo) ClaimLeaseExpired(ctx context.Context, id string, lease time.Duration) (bool, error) {
	return r.inner.ClaimLeaseExpired(ctx, id, lease)
}
func (r *countdownJobRepo) InvalidateClaim(ctx context.Context, id string) error {
	return r.inner.InvalidateClaim(ctx, id)
}
func (r *countdownJobRepo) Update(ctx context.Context, j domain.GenerationJob, expected string) error {
	return r.inner.Update(ctx, j, expected)
}
func (r *countdownJobRepo) PersistArtwork(ctx context.Context, j domain.GenerationJob, artwork domain.Asset) ([]ports.DeletedBlobRef, error) {
	return r.inner.PersistArtwork(ctx, j, artwork)
}
func (r *countdownJobRepo) CompleteSuccess(ctx context.Context, j domain.GenerationJob, mockup domain.Asset, design domain.Design) ([]ports.DeletedBlobRef, error) {
	return r.inner.CompleteSuccess(ctx, j, mockup, design)
}
func (r *countdownJobRepo) FinalizeSuccess(ctx context.Context, j domain.GenerationJob, design domain.Design, mockup domain.Asset) ([]ports.DeletedBlobRef, error) {
	return r.inner.FinalizeSuccess(ctx, j, design, mockup)
}
func (r *countdownJobRepo) CompleteFailure(ctx context.Context, j domain.GenerationJob, design domain.Design) error {
	return r.inner.CompleteFailure(ctx, j, design)
}
func (r *countdownJobRepo) FailActive(ctx context.Context, id, designID, terminalReason, errorCode string, claimToken int64, at time.Time) error {
	return r.inner.FailActive(ctx, id, designID, terminalReason, errorCode, claimToken, at)
}

func TestDesignService_Approve_ViewCooldownFailIncludesVersionID(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-11111111111a"
	uploadID := "22222222-2222-4222-8222-22222222222a"
	artID := "33333333-3333-4333-8333-33333333333a"
	jobID := "55555555-5555-4555-8555-55555555555a"
	srcKey := "src-approve-view"
	artBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 9}
	finished := now.Add(-time.Minute)
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: srcKey,
			MimeType: "image/jpeg", SizeBytes: int64(len(jpegStub)), Width: 300, Height: 300,
			Checksum: sha256Sum(jpegStub), DeleteAfter: now.Add(time.Hour),
		},
		artID: {
			ID: artID, Type: domain.AssetTypeArtwork, StorageKey: "art-approve-view",
			MimeType: "image/png", SizeBytes: int64(len(artBytes)), Width: 64, Height: 64,
			Checksum: sha256Sum(artBytes), DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	innerJobs := &designJobRepo{byID: map[string]domain.GenerationJob{}}
	jobs := &countdownJobRepo{inner: innerJobs, failAfter: 1} // approve load once; view lookup fails
	versions := &versionMemRepo{designs: designs, assets: assets}
	svc := &application.DesignService{
		Designs: &bindingDesignRepo{designs: designs, assets: assets}, Assets: assets,
		Jobs: jobs, Versions: versions,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{srcKey: append([]byte(nil), jpegStub...), "art-approve-view": append([]byte(nil), artBytes...)}},
		IDs:       testutil.NewSeqGenerator("des"), Clock: testutil.NewFakeClock(now),
		Cooldown: 30 * time.Second, MaxAttempts: 3,
		EnqueuePrepare: func(context.Context, string, string) error { return nil },
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assets.mu.Lock()
	a := assets.byID[artID]
	a.OwnerRef = created.ID
	assets.byID[artID] = a
	assets.mu.Unlock()
	d, _, _ := designs.GetByID(context.Background(), created.ID)
	d.Status = domain.DesignPreviewReady
	d.LatestJobID = jobID
	d.GenerationAttempts = 1
	_ = designs.Insert(context.Background(), d)
	innerJobs.byID[jobID] = domain.GenerationJob{
		ID: jobID, DesignID: created.ID, Status: domain.JobSucceeded, ArtworkAssetID: artID,
		Provider: "fake", Model: "fake", PromptVersion: "pv1", ParametersJSON: []byte(`{}`),
		FinishedAt: &finished,
	}
	res, err := svc.Approve(context.Background(), created.ID, created.OwnerToken)
	var de *application.DesignError
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrUnavailable || de.VersionID == "" {
		t.Fatalf("want unavailable with version_id, got %v", err)
	}
	if res.Version.ID != de.VersionID {
		t.Fatalf("version on result=%q error=%q", res.Version.ID, de.VersionID)
	}
	if res.Design.ID != "" {
		t.Fatalf("approve must omit design view on cooldown failure: %+v", res.Design)
	}
	d2, ok, _ := designs.GetByID(context.Background(), created.ID)
	if !ok || d2.Status != domain.DesignApproved {
		t.Fatalf("approve must still commit: %+v", d2)
	}
}

type gatedAssetRepo struct {
	inner *designAssetRepo
	fail  bool
	err   error
}

func (g *gatedAssetRepo) Insert(ctx context.Context, a domain.Asset) error {
	return g.inner.Insert(ctx, a)
}
func (g *gatedAssetRepo) GetByID(ctx context.Context, id string) (domain.Asset, bool, error) {
	if g.fail {
		return domain.Asset{}, false, g.err
	}
	return g.inner.GetByID(ctx, id)
}
func (g *gatedAssetRepo) SoftDelete(ctx context.Context, id string, at time.Time) error {
	return g.inner.SoftDelete(ctx, id, at)
}
func (g *gatedAssetRepo) BindOwnerRef(ctx context.Context, assetID, ownerRef string, deleteAfter time.Time) error {
	return g.inner.BindOwnerRef(ctx, assetID, ownerRef, deleteAfter)
}
func (g *gatedAssetRepo) ClearOwnerRef(ctx context.Context, assetID, ownerRef string) error {
	return g.inner.ClearOwnerRef(ctx, assetID, ownerRef)
}
func (g *gatedAssetRepo) ClearStorageKey(ctx context.Context, assetID string) error {
	return g.inner.ClearStorageKey(ctx, assetID)
}

func TestDesignService_Patch_PostCommitViewIncludesDesign(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-11111111111b"
	uploadID := "22222222-2222-4222-8222-22222222222b"
	jobID := "55555555-5555-4555-8555-55555555555b"
	finished := now.Add(-time.Minute)
	innerAssets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: "src-patch-view",
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	viewBoom := errors.New("postgres dial timeout")
	assets := &gatedAssetRepo{inner: innerAssets}
	designs := &designMemRepo{}
	jobs := &designJobRepo{byID: map[string]domain.GenerationJob{}}
	svc := &application.DesignService{
		Designs: &bindingDesignRepo{designs: designs, assets: innerAssets}, Assets: assets, Jobs: jobs,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{"src-patch-view": {1}}},
		IDs:       testutil.NewSeqGenerator("des"), Clock: testutil.NewFakeClock(now),
		Cooldown: 30 * time.Second, MaxAttempts: 3,
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	d, _, _ := designs.GetByID(context.Background(), created.ID)
	d.Status = domain.DesignPreviewReady
	d.LatestJobID = jobID
	d.GenerationAttempts = 1
	_ = designs.Insert(context.Background(), d)
	jobs.byID[jobID] = domain.GenerationJob{
		ID: jobID, DesignID: created.ID, Status: domain.JobSucceeded, Attempt: 1,
		FinishedAt: &finished,
	}
	assets.fail = true
	assets.err = viewBoom
	_, err = svc.Patch(context.Background(), created.ID, created.OwnerToken, application.PatchInput{
		Texts: map[string]string{"dog_name": "Reksio"},
	})
	var de *application.DesignError
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrUnavailable || de.Design.ID != created.ID {
		t.Fatalf("want unavailable with committed design, got %v", err)
	}
	if de.Design.Texts["dog_name"] != "Reksio" {
		t.Fatalf("committed design texts: %+v", de.Design.Texts)
	}
	// Cooldown preflight succeeded (elapsed) — keep honest retry_after:0.
	if de.Design.RetryAfter != 0 {
		t.Fatalf("view fail after cooldown ok must keep retry_after=0, got %d", de.Design.RetryAfter)
	}
	if !errors.Is(err, viewBoom) {
		t.Fatalf("want wrapped view cause, got %v", err)
	}
	d2, ok, _ := designs.GetByID(context.Background(), created.ID)
	if !ok || d2.Texts["dog_name"] != "Reksio" {
		t.Fatalf("patch must persist before view fail: %+v", d2)
	}
}

func TestDesignService_Get_JobLookupErrorPreservesCause(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-11111111111c"
	uploadID := "22222222-2222-4222-8222-22222222222c"
	jobID := "55555555-5555-4555-8555-55555555555c"
	boom := errors.New("postgres dial timeout")
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: "src-job-err",
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	inner := &designJobRepo{byID: map[string]domain.GenerationJob{}}
	svc := &application.DesignService{
		Designs: &bindingDesignRepo{designs: designs, assets: assets}, Assets: assets,
		Jobs: &errDesignJobRepo{inner: inner, err: boom},
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{"src-job-err": {1}}},
		IDs:       testutil.NewSeqGenerator("des"), Clock: testutil.NewFakeClock(now),
		Cooldown: 30 * time.Second, MaxAttempts: 3,
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	d, _, _ := designs.GetByID(context.Background(), created.ID)
	d.Status = domain.DesignPreviewReady
	d.LatestJobID = jobID
	d.GenerationAttempts = 1
	_ = designs.Insert(context.Background(), d)
	_, err = svc.Get(context.Background(), created.ID, created.OwnerToken)
	var de *application.DesignError
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrUnavailable {
		t.Fatalf("want unavailable, got %v", err)
	}
	if de.DesignID != created.ID || de.JobID != jobID {
		t.Fatalf("want design_id/job_id on error, got design=%q job=%q", de.DesignID, de.JobID)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped cause %v, got %v", boom, err)
	}
}

func TestDesignService_Get_ArtworkLookupError(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-11111111111f"
	uploadID := "22222222-2222-4222-8222-22222222222f"
	artID := "33333333-3333-4333-8333-33333333333f"
	jobID := "55555555-5555-4555-8555-55555555555f"
	boom := errors.New("postgres dial timeout")
	inner := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: "src-art-err",
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
		artID: {
			ID: artID, Type: domain.AssetTypeArtwork, StorageKey: "art-err",
			MimeType: "image/png", SizeBytes: 1, Width: 64, Height: 64, Checksum: "sha256:art",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	jobs := &designJobRepo{byID: map[string]domain.GenerationJob{}}
	svc := &application.DesignService{
		Designs: &bindingDesignRepo{designs: designs, assets: inner},
		Assets:  &errAssetRepo{designAssetRepo: *inner, getErr: boom, failID: artID},
		Jobs:    jobs,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{"src-art-err": {1}, "art-err": {2}}},
		IDs:       testutil.NewSeqGenerator("des"), Clock: testutil.NewFakeClock(now),
		Cooldown: 0, MaxAttempts: 3,
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	inner.mu.Lock()
	a := inner.byID[artID]
	a.OwnerRef = created.ID
	inner.byID[artID] = a
	inner.mu.Unlock()
	d, _, _ := designs.GetByID(context.Background(), created.ID)
	d.Status = domain.DesignPreviewReady
	d.LatestJobID = jobID
	d.GenerationAttempts = 1
	_ = designs.Insert(context.Background(), d)
	finished := now.Add(-time.Minute)
	jobs.byID[jobID] = domain.GenerationJob{
		ID: jobID, DesignID: created.ID, Status: domain.JobSucceeded, Attempt: 1,
		ArtworkAssetID: artID, FinishedAt: &finished,
	}
	_, err = svc.Get(context.Background(), created.ID, created.OwnerToken)
	var de *application.DesignError
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrUnavailable {
		t.Fatalf("want unavailable, got %v", err)
	}
	if de.DesignID != created.ID || de.JobID != jobID {
		t.Fatalf("want design_id/job_id, got design=%q job=%q", de.DesignID, de.JobID)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped cause %v, got %v", boom, err)
	}
}

func TestDesignService_Get_PreviewJobErrorWhenCooldownDisabled(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-11111111111d"
	uploadID := "22222222-2222-4222-8222-22222222222d"
	jobID := "55555555-5555-4555-8555-55555555555d"
	boom := errors.New("postgres dial timeout")
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: "src-preview-err",
			MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:ab",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	inner := &designJobRepo{byID: map[string]domain.GenerationJob{}}
	svc := &application.DesignService{
		Designs: &bindingDesignRepo{designs: designs, assets: assets}, Assets: assets,
		Jobs: &errDesignJobRepo{inner: inner, err: boom},
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{"src-preview-err": {1}}},
		IDs:       testutil.NewSeqGenerator("des"), Clock: testutil.NewFakeClock(now),
		Cooldown: 0, MaxAttempts: 3, // cooldown skipped — preview job read must still fail closed
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	d, _, _ := designs.GetByID(context.Background(), created.ID)
	d.Status = domain.DesignPreviewReady
	d.LatestJobID = jobID
	d.GenerationAttempts = 1
	_ = designs.Insert(context.Background(), d)
	_, err = svc.Get(context.Background(), created.ID, created.OwnerToken)
	var de *application.DesignError
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrUnavailable {
		t.Fatalf("want unavailable from preview job read, got %v", err)
	}
	if de.DesignID != created.ID || de.JobID != jobID {
		t.Fatalf("want design_id/job_id, got design=%q job=%q", de.DesignID, de.JobID)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped cause %v, got %v", boom, err)
	}
}


func TestDesignService_Approve_PreviewJobErrorWhenCooldownDisabled(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-11111111111e"
	uploadID := "22222222-2222-4222-8222-22222222222e"
	artID := "33333333-3333-4333-8333-33333333333e"
	jobID := "55555555-5555-4555-8555-55555555555e"
	srcKey := "src-approve-cd0"
	artBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 9}
	finished := now.Add(-time.Minute)
	boom := errors.New("postgres dial timeout")
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: srcKey,
			MimeType: "image/jpeg", SizeBytes: int64(len(jpegStub)), Width: 300, Height: 300,
			Checksum: sha256Sum(jpegStub), DeleteAfter: now.Add(time.Hour),
		},
		artID: {
			ID: artID, Type: domain.AssetTypeArtwork, StorageKey: "art-approve-cd0",
			MimeType: "image/png", SizeBytes: int64(len(artBytes)), Width: 64, Height: 64,
			Checksum: sha256Sum(artBytes), DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	innerJobs := &designJobRepo{byID: map[string]domain.GenerationJob{}}
	jobs := &countdownJobRepo{inner: innerJobs, failAfter: 1, errAfter: boom} // approve load ok; preview I/O fails
	versions := &versionMemRepo{designs: designs, assets: assets}
	svc := &application.DesignService{
		Designs: &bindingDesignRepo{designs: designs, assets: assets}, Assets: assets,
		Jobs: jobs, Versions: versions,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{srcKey: append([]byte(nil), jpegStub...), "art-approve-cd0": append([]byte(nil), artBytes...)}},
		IDs:       testutil.NewSeqGenerator("des"), Clock: testutil.NewFakeClock(now),
		Cooldown: 0, MaxAttempts: 3, // cooldown skipped — preview job read must still fail closed
		EnqueuePrepare: func(context.Context, string, string) error { return nil },
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assets.mu.Lock()
	a := assets.byID[artID]
	a.OwnerRef = created.ID
	assets.byID[artID] = a
	assets.mu.Unlock()
	d, _, _ := designs.GetByID(context.Background(), created.ID)
	d.Status = domain.DesignPreviewReady
	d.LatestJobID = jobID
	d.GenerationAttempts = 1
	_ = designs.Insert(context.Background(), d)
	innerJobs.byID[jobID] = domain.GenerationJob{
		ID: jobID, DesignID: created.ID, Status: domain.JobSucceeded, ArtworkAssetID: artID,
		Provider: "fake", Model: "fake", PromptVersion: "pv1", ParametersJSON: []byte(`{}`),
		FinishedAt: &finished,
	}
	res, err := svc.Approve(context.Background(), created.ID, created.OwnerToken)
	var de *application.DesignError
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrUnavailable || de.VersionID == "" {
		t.Fatalf("want unavailable with version_id, got %v", err)
	}
	if res.Design.ID != "" {
		t.Fatalf("must omit design view: %+v", res.Design)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped cause, got %v", err)
	}
	d2, ok, _ := designs.GetByID(context.Background(), created.ID)
	if !ok || d2.Status != domain.DesignApproved {
		t.Fatalf("approve must still commit: %+v", d2)
	}
}

func TestDesignService_Approve_EnqueueFailReturnsUnavailable(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111111"
	uploadID := "22222222-2222-4222-8222-222222222222"
	artID := "33333333-3333-4333-8333-333333333333"
	jobID := "55555555-5555-4555-8555-555555555555"
	srcKey := "src-k"
	artBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 9}
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: srcKey,
			MimeType: "image/jpeg", SizeBytes: int64(len(jpegStub)), Width: 300, Height: 300,
			Checksum: sha256Sum(jpegStub), DeleteAfter: now.Add(time.Hour),
		},
		artID: {
			ID: artID, Type: domain.AssetTypeArtwork, StorageKey: "art-k",
			MimeType: "image/png", SizeBytes: int64(len(artBytes)), Width: 64, Height: 64,
			Checksum: sha256Sum(artBytes), DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	versions := &versionMemRepo{designs: designs, assets: assets}
	svc := &application.DesignService{
		Designs: &bindingDesignRepo{designs: designs, assets: assets}, Assets: assets,
		Jobs: &designJobRepo{byID: map[string]domain.GenerationJob{}}, Versions: versions,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{srcKey: append([]byte(nil), jpegStub...), "art-k": append([]byte(nil), artBytes...)}},
		IDs:       testutil.NewSeqGenerator("des"), Clock: testutil.NewFakeClock(now),
		EnqueuePrepare: func(context.Context, string, string) error { return errors.New("queue down") },
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assets.mu.Lock()
	a := assets.byID[artID]
	a.OwnerRef = created.ID
	assets.byID[artID] = a
	assets.mu.Unlock()
	d, _, _ := designs.GetByID(context.Background(), created.ID)
	d.Status = domain.DesignPreviewReady
	d.LatestJobID = jobID
	_ = designs.Insert(context.Background(), d)
	svc.Jobs.(*designJobRepo).byID[jobID] = domain.GenerationJob{
		ID: jobID, DesignID: created.ID, Status: domain.JobSucceeded, ArtworkAssetID: artID,
		Provider: "fake", Model: "fake", PromptVersion: "pv1", ParametersJSON: []byte(`{}`),
	}
	_, err = svc.Approve(context.Background(), created.ID, created.OwnerToken)
	var de *application.DesignError
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrUnavailable || de.VersionID == "" {
		t.Fatalf("want 503-shaped unavailable with version_id, got %v", err)
	}
	// Version committed; idempotent retry must also surface enqueue failure.
	_, err = svc.Approve(context.Background(), created.ID, created.OwnerToken)
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrUnavailable || de.VersionID == "" {
		t.Fatalf("idempotent enqueue fail: %v", err)
	}
}

func TestDesignService_Approve_ChecksumMismatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111111"
	uploadID := "22222222-2222-4222-8222-222222222222"
	artID := "33333333-3333-4333-8333-333333333333"
	jobID := "55555555-5555-4555-8555-555555555555"
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: "src",
			MimeType: "image/jpeg", SizeBytes: int64(len(jpegStub)), Checksum: sha256Sum(jpegStub),
			DeleteAfter: now.Add(time.Hour),
		},
		artID: {
			ID: artID, Type: domain.AssetTypeArtwork, StorageKey: "art-k",
			MimeType: "image/png", SizeBytes: 3, Checksum: "sha256:deadbeef",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	svc := &application.DesignService{
		Designs: &bindingDesignRepo{designs: designs, assets: assets}, Assets: assets,
		Jobs:      &designJobRepo{byID: map[string]domain.GenerationJob{}},
		Versions:  &versionMemRepo{designs: designs, assets: assets},
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{"src": jpegStub, "art-k": {1, 2, 3}}},
		IDs:       testutil.NewSeqGenerator("des"), Clock: testutil.NewFakeClock(now),
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assets.mu.Lock()
	a := assets.byID[artID]
	a.OwnerRef = created.ID
	assets.byID[artID] = a
	assets.mu.Unlock()
	d, _, _ := designs.GetByID(context.Background(), created.ID)
	d.Status = domain.DesignPreviewReady
	d.LatestJobID = jobID
	_ = designs.Insert(context.Background(), d)
	svc.Jobs.(*designJobRepo).byID[jobID] = domain.GenerationJob{
		ID: jobID, DesignID: created.ID, Status: domain.JobSucceeded, ArtworkAssetID: artID,
		Provider: "fake", Model: "fake", PromptVersion: "pv1",
	}
	_, err = svc.Approve(context.Background(), created.ID, created.OwnerToken)
	var de *application.DesignError
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrChecksumMismatch {
		t.Fatalf("want checksum_mismatch got %v", err)
	}
}

func TestDesignService_Approve_SkipsEnqueueWhenProductionReady(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111111"
	uploadID := "22222222-2222-4222-8222-222222222222"
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: "src",
			MimeType: "image/jpeg", SizeBytes: int64(len(jpegStub)), Checksum: sha256Sum(jpegStub),
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	versions := &versionMemRepo{designs: designs, assets: assets}
	enqueues := 0
	svc := &application.DesignService{
		Designs: &bindingDesignRepo{designs: designs, assets: assets}, Assets: assets,
		Jobs: &designJobRepo{byID: map[string]domain.GenerationJob{}}, Versions: versions,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{"src": jpegStub}},
		IDs:       testutil.NewSeqGenerator("des"), Clock: testutil.NewFakeClock(now),
		EnqueuePrepare: func(context.Context, string, string) error {
			enqueues++
			return nil
		},
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	d, _, _ := designs.GetByID(context.Background(), created.ID)
	d.Status = domain.DesignApproved
	d.ApprovedVersionID = "ver-ready"
	_ = designs.Insert(context.Background(), d)
	versions.mu.Lock()
	versions.byID = map[string]domain.DesignVersion{"ver-ready": {
		ID: "ver-ready", DesignID: created.ID, TemplateID: tplID, TemplateVersion: 1,
		ArtworkAssetID: "art", ArtworkChecksum: "sha256:x", ProductionStatus: domain.ProductionReady,
		Provider: "fake", Model: "m", PromptVersion: "pv1", ApprovedAt: now,
	}}
	versions.byDesign = map[string]string{created.ID: "ver-ready"}
	versions.mu.Unlock()
	if _, err := svc.Approve(context.Background(), created.ID, created.OwnerToken); err != nil {
		t.Fatal(err)
	}
	if enqueues != 0 {
		t.Fatalf("ready production must not enqueue: %d", enqueues)
	}
}

func TestDesignService_Clone_CleanupStopsWhenSoftDeleteFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111111"
	uploadID := "22222222-2222-4222-8222-222222222222"
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: "src",
			MimeType: "image/jpeg", SizeBytes: int64(len(jpegStub)), Checksum: sha256Sum(jpegStub),
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	store := &designBlobStore{objects: map[string][]byte{"src": append([]byte(nil), jpegStub...)}}
	failSoft := &failSoftDeleteAssets{inner: assets, failAfterInsert: true}
	svc := &application.DesignService{
		Designs:   &failCreateBound{inner: &bindingDesignRepo{designs: designs, assets: assets}},
		Assets:    failSoft,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     store, IDs: testutil.NewSeqGenerator("des"), Clock: testutil.NewFakeClock(now),
	}
	// Seed approved design without going through Create (binding would attach upload).
	token, hash, err := application.MintOwnerToken()
	if err != nil {
		t.Fatal(err)
	}
	designID := "des-approved"
	assets.mu.Lock()
	a := assets.byID[uploadID]
	a.OwnerRef = designID
	assets.byID[uploadID] = a
	assets.mu.Unlock()
	_ = designs.Insert(context.Background(), domain.Design{
		ID: designID, TemplateID: tplID, TemplateVersion: 2, Status: domain.DesignApproved,
		Texts: map[string]string{"dog_name": "Burek", "tagline": "METAL"}, SourceAssetID: uploadID,
		OwnerTokenHash: hash, ApprovedVersionID: "ver-1",
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(domain.DesignRetentionUnordered),
	})
	_, err = svc.Clone(context.Background(), designID, token)
	if err == nil {
		t.Fatal("want create bind failure")
	}
	// SoftDelete failed → blob must remain (upload rule).
	if len(store.objects) < 2 {
		t.Fatalf("clone Put blob must remain when SoftDelete fails: keys=%v", store.objects)
	}
	if _, ok := assets.byID[failSoft.lastInserted]; !ok {
		t.Fatal("asset row must remain active when SoftDelete fails")
	}
	if assets.byID[failSoft.lastInserted].DeletedAt != nil {
		t.Fatal("must not soft-delete when SoftDelete returned error")
	}
}

type failSoftDeleteAssets struct {
	inner           *designAssetRepo
	failID          string
	failAfterInsert bool
	lastInserted    string
}

func (f *failSoftDeleteAssets) Insert(ctx context.Context, a domain.Asset) error {
	if err := f.inner.Insert(ctx, a); err != nil {
		return err
	}
	f.lastInserted = a.ID
	return nil
}
func (f *failSoftDeleteAssets) GetByID(ctx context.Context, id string) (domain.Asset, bool, error) {
	return f.inner.GetByID(ctx, id)
}
func (f *failSoftDeleteAssets) SoftDelete(ctx context.Context, id string, at time.Time) error {
	if f.failAfterInsert && id == f.lastInserted {
		return errors.New("soft-delete failed")
	}
	if f.failID != "" && id == f.failID {
		return errors.New("soft-delete failed")
	}
	return f.inner.SoftDelete(ctx, id, at)
}
func (f *failSoftDeleteAssets) BindOwnerRef(ctx context.Context, assetID, ownerRef string, deleteAfter time.Time) error {
	return f.inner.BindOwnerRef(ctx, assetID, ownerRef, deleteAfter)
}
func (f *failSoftDeleteAssets) ClearOwnerRef(ctx context.Context, assetID, ownerRef string) error {
	return f.inner.ClearOwnerRef(ctx, assetID, ownerRef)
}
func (f *failSoftDeleteAssets) ClearStorageKey(ctx context.Context, assetID string) error {
	return f.inner.ClearStorageKey(ctx, assetID)
}

type failCreateBound struct {
	inner ports.DesignRepository
}

func (f *failCreateBound) Insert(ctx context.Context, d domain.Design) error {
	return f.inner.Insert(ctx, d)
}
func (f *failCreateBound) GetByID(ctx context.Context, id string) (domain.Design, bool, error) {
	return f.inner.GetByID(ctx, id)
}
func (f *failCreateBound) UpdateEditable(ctx context.Context, d domain.Design, expectedSource string, expectedUpdatedAt, deleteAfter time.Time) error {
	return f.inner.UpdateEditable(ctx, d, expectedSource, expectedUpdatedAt, deleteAfter)
}
func (f *failCreateBound) CreateBound(context.Context, domain.Design, time.Time) error {
	return errors.New("create bind failed")
}
func (f *failCreateBound) ReplaceSource(ctx context.Context, d domain.Design, old string, expectedUpdatedAt, deleteAfter time.Time) (string, error) {
	return f.inner.ReplaceSource(ctx, d, old, expectedUpdatedAt, deleteAfter)
}

func TestDesignService_Clone_OwnerRefMismatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tplID := "11111111-1111-4111-8111-111111111111"
	uploadID := "22222222-2222-4222-8222-222222222222"
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: "src",
			MimeType: "image/jpeg", SizeBytes: int64(len(jpegStub)), Checksum: sha256Sum(jpegStub),
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	designs := &designMemRepo{}
	svc := &application.DesignService{
		Designs: &bindingDesignRepo{designs: designs, assets: assets}, Assets: assets,
		Templates: &application.TemplateCatalog{Store: &designTplStore{rows: map[string]domain.Template{tplID: publishedTpl(tplID)}}},
		Store:     &designBlobStore{objects: map[string][]byte{"src": jpegStub}},
		IDs:       testutil.NewSeqGenerator("des"), Clock: testutil.NewFakeClock(now),
	}
	created, err := svc.Create(context.Background(), application.CreateInput{
		TemplateID: tplID, UploadID: uploadID, Texts: map[string]string{"dog_name": "Burek"},
	})
	if err != nil {
		t.Fatal(err)
	}
	d, _, _ := designs.GetByID(context.Background(), created.ID)
	d.Status = domain.DesignApproved
	d.ApprovedVersionID = "ver-1"
	_ = designs.Insert(context.Background(), d)
	assets.mu.Lock()
	a := assets.byID[uploadID]
	a.OwnerRef = "other-design"
	assets.byID[uploadID] = a
	assets.mu.Unlock()
	_, err = svc.Clone(context.Background(), created.ID, created.OwnerToken)
	var de *application.DesignError
	if err == nil || !errors.As(err, &de) || de.Code != application.DesignErrUploadUnavailable {
		t.Fatalf("want upload_unavailable got %v", err)
	}
}
