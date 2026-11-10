package application_test

import (
	"context"
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
}

func (m *errAssetRepo) GetByID(ctx context.Context, id string) (domain.Asset, bool, error) {
	if m.getErr != nil {
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
func (r *designJobRepo) BeginGenerating(context.Context, domain.GenerationJob, domain.Design, time.Time) error {
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
func (r *errDesignJobRepo) BeginGenerating(ctx context.Context, j domain.GenerationJob, d domain.Design, at time.Time) error {
	return r.inner.BeginGenerating(ctx, j, d, at)
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
