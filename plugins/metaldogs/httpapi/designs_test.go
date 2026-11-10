package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

type dAssetRepo struct {
	mu   sync.Mutex
	byID map[string]domain.Asset
}

func (m *dAssetRepo) Insert(_ context.Context, a domain.Asset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byID == nil {
		m.byID = map[string]domain.Asset{}
	}
	m.byID[a.ID] = a
	return nil
}
func (m *dAssetRepo) GetByID(_ context.Context, id string) (domain.Asset, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[id]
	if !ok || a.DeletedAt != nil {
		return domain.Asset{}, false, nil
	}
	return a, true, nil
}
func (m *dAssetRepo) SoftDelete(context.Context, string, time.Time) error { return nil }
func (m *dAssetRepo) BindOwnerRef(_ context.Context, assetID, ownerRef string) error {
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
func (m *dAssetRepo) ClearOwnerRef(_ context.Context, assetID, ownerRef string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[assetID]
	if ok && a.OwnerRef == ownerRef {
		a.OwnerRef = ""
		m.byID[assetID] = a
	}
	return nil
}

type dDesignRepo struct {
	mu     sync.Mutex
	byID   map[string]domain.Design
	assets *dAssetRepo
}

func (m *dDesignRepo) Insert(_ context.Context, d domain.Design) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byID == nil {
		m.byID = map[string]domain.Design{}
	}
	cp := d
	cp.OwnerTokenHash = append([]byte(nil), d.OwnerTokenHash...)
	m.byID[d.ID] = cp
	return nil
}
func (m *dDesignRepo) GetByID(_ context.Context, id string) (domain.Design, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.byID[id]
	if !ok {
		return domain.Design{}, false, nil
	}
	cp := d
	cp.OwnerTokenHash = append([]byte(nil), d.OwnerTokenHash...)
	return cp, true, nil
}
func (m *dDesignRepo) UpdateEditable(_ context.Context, d domain.Design) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.byID[d.ID]
	cur.Texts = d.Texts
	cur.SourceAssetID = d.SourceAssetID
	cur.Status = d.Status
	cur.UpdatedAt = d.UpdatedAt
	m.byID[d.ID] = cur
	return nil
}
func (m *dDesignRepo) CreateBound(ctx context.Context, d domain.Design) error {
	if err := m.assets.BindOwnerRef(ctx, d.SourceAssetID, d.ID); err != nil {
		return err
	}
	return m.Insert(ctx, d)
}
func (m *dDesignRepo) ReplaceSource(ctx context.Context, d domain.Design, old string) error {
	if old != "" && old != d.SourceAssetID {
		_ = m.assets.ClearOwnerRef(ctx, old, d.ID)
	}
	if err := m.assets.BindOwnerRef(ctx, d.SourceAssetID, d.ID); err != nil {
		return err
	}
	return m.UpdateEditable(ctx, d)
}

type dBlob struct{ objects map[string][]byte }

func (m *dBlob) Put(_ context.Context, key string, r io.Reader, _ string) error {
	b, _ := io.ReadAll(r)
	if m.objects == nil {
		m.objects = map[string][]byte{}
	}
	m.objects[key] = b
	return nil
}
func (m *dBlob) Head(_ context.Context, key string) error {
	if _, ok := m.objects[key]; !ok {
		return fmt.Errorf("missing")
	}
	return nil
}
func (m *dBlob) SignedGetURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://signed.example/" + key, nil
}
func (m *dBlob) Delete(_ context.Context, key string) error { delete(m.objects, key); return nil }

func designSvc(t *testing.T, tplID, uploadID string, tplStatus string) (*application.DesignService, string) {
	t.Helper()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	key := "private/blobs/metaldogs/source/2026/11/" + uploadID + ".jpg"
	assets := &dAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: key,
			MimeType: "image/jpeg", SizeBytes: 10, Width: 300, Height: 300, Checksum: "sha256:x",
			DeleteAfter: now.Add(time.Hour),
		},
	}}
	tpl := samplePublished(tplID, "head-front", "black_metal")
	tpl.Status = tplStatus
	tpl.Version = 3
	store := &tplMem{rows: map[string]domain.Template{tplID: tpl}}
	svc := &application.DesignService{
		Designs:   &dDesignRepo{assets: assets},
		Assets:    assets,
		Templates: &application.TemplateCatalog{Store: store},
		Store:     &dBlob{objects: map[string][]byte{key: {1}}},
		IDs:       ports.UUIDGenerator{},
		Clock:     ports.SystemClock{},
	}
	return svc, key
}

func TestDesigns_CreateGetAuthPatch(t *testing.T) {
	t.Parallel()
	tplID := "aaaaaaaa-bbbb-4ccc-8ddd-111111111111"
	uploadID := "aaaaaaaa-bbbb-4ccc-8ddd-222222222222"
	svc, _ := designSvc(t, tplID, uploadID, domain.TemplatePublished)

	mux := http.NewServeMux()
	mux.Handle("POST /api/customizer/designs", httpapi.CreateDesign(svc))
	mux.Handle("GET /api/customizer/designs/{id}", httpapi.GetDesign(svc))
	mux.Handle("PATCH /api/customizer/designs/{id}", httpapi.PatchDesign(svc))

	body, _ := json.Marshal(map[string]any{
		"template_id": tplID,
		"upload_id":   uploadID,
		"texts":       map[string]string{"dog_name": "Burek"},
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/customizer/designs", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var created httpapi.DesignResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.OwnerToken == "" || created.ID == "" || created.SourcePreviewURL == "" {
		t.Fatalf("%+v", created)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/customizer/designs/"+created.ID, nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing bearer want 404 got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/customizer/designs/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer wrong")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("wrong bearer want 404 got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/customizer/designs/"+created.ID+"?owner_token="+created.OwnerToken, nil)
	req.Header.Set("Authorization", "Bearer "+created.OwnerToken)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("token in query want 400 got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/customizer/designs/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+created.OwnerToken)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get %d %s", rec.Code, rec.Body.String())
	}
	var got httpapi.DesignResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.OwnerToken != "" || !strings.Contains(got.SourcePreviewURL, "signed.example") {
		t.Fatalf("%+v", got)
	}

	patch, _ := json.Marshal(map[string]any{"texts": map[string]string{"dog_name": "Azor"}})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/customizer/designs/"+created.ID, bytes.NewReader(patch))
	req.Header.Set("Authorization", "Bearer "+created.OwnerToken)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Texts["dog_name"] != "Azor" {
		t.Fatalf("%+v", got)
	}
}

func TestDesigns_Create_UnpublishedTemplate(t *testing.T) {
	t.Parallel()
	tplID := "aaaaaaaa-bbbb-4ccc-8ddd-333333333333"
	uploadID := "aaaaaaaa-bbbb-4ccc-8ddd-444444444444"
	svc, _ := designSvc(t, tplID, uploadID, domain.TemplateDraft)
	body, _ := json.Marshal(map[string]any{
		"template_id": tplID, "upload_id": uploadID, "texts": map[string]string{"dog_name": "Burek"},
	})
	rec := httptest.NewRecorder()
	httpapi.CreateDesign(svc).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/customizer/designs", bytes.NewReader(body)))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	assertErrCode(t, rec, application.DesignErrTemplateUnavailable)
}

func TestDesigns_NilService(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	httpapi.CreateDesign(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/customizer/designs", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("%d", rec.Code)
	}
}
