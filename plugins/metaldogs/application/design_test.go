package application_test

import (
	"context"
	"errors"
	"fmt"
	"io"
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

func (m *designMemRepo) UpdateEditable(_ context.Context, d domain.Design) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.byID[d.ID]
	if !ok {
		return fmt.Errorf("missing")
	}
	cur.Texts = copyTexts(d.Texts)
	cur.SourceAssetID = d.SourceAssetID
	cur.Status = d.Status
	cur.UpdatedAt = d.UpdatedAt
	m.byID[d.ID] = cur
	return nil
}

func (m *designMemRepo) CreateBound(ctx context.Context, d domain.Design) error {
	if err := m.Insert(ctx, d); err != nil {
		return err
	}
	return nil // bind done by DesignService via Assets in real path; mem uses CreateBound only
}

func (m *designMemRepo) ReplaceSource(ctx context.Context, d domain.Design, _ string) error {
	return m.UpdateEditable(ctx, d)
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
	a.DeletedAt = &at
	m.byID[id] = a
	return nil
}
func (m *designAssetRepo) BindOwnerRef(_ context.Context, assetID, ownerRef string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[assetID]
	if !ok || a.DeletedAt != nil || a.Type != domain.AssetTypeSource || a.OwnerRef != "" {
		return ports.ErrAssetBindConflict
	}
	a.OwnerRef = ownerRef
	m.byID[assetID] = a
	return nil
}
func (m *designAssetRepo) ClearOwnerRef(_ context.Context, assetID, ownerRef string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[assetID]
	if ok && a.OwnerRef == ownerRef {
		a.OwnerRef = ""
		m.byID[assetID] = a
	}
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
func (m *bindingDesignRepo) UpdateEditable(ctx context.Context, d domain.Design) error {
	return m.designs.UpdateEditable(ctx, d)
}
func (m *bindingDesignRepo) CreateBound(ctx context.Context, d domain.Design) error {
	if err := m.assets.BindOwnerRef(ctx, d.SourceAssetID, d.ID); err != nil {
		return err
	}
	return m.designs.Insert(ctx, d)
}
func (m *bindingDesignRepo) ReplaceSource(ctx context.Context, d domain.Design, oldAssetID string) error {
	if oldAssetID != "" && oldAssetID != d.SourceAssetID {
		_ = m.assets.ClearOwnerRef(ctx, oldAssetID, d.ID)
	}
	if err := m.assets.BindOwnerRef(ctx, d.SourceAssetID, d.ID); err != nil {
		return err
	}
	return m.designs.UpdateEditable(ctx, d)
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
