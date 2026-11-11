package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func httpSHA(b []byte) string {
	sum := sha256.Sum256(b)
	return application.ChecksumSHA256Prefix + hex.EncodeToString(sum[:])
}

var httpJPEG = []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46}
var httpPNG = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2}

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
func (m *dAssetRepo) SoftDelete(_ context.Context, id string, at time.Time) error {
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
func (m *dAssetRepo) BindOwnerRef(_ context.Context, assetID, ownerRef string, deleteAfter time.Time) error {
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
func (m *dAssetRepo) ClearOwnerRef(_ context.Context, assetID, ownerRef string) error {
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
func (m *dAssetRepo) ClearStorageKey(_ context.Context, assetID string) error {
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
func (m *dDesignRepo) UpdateEditable(_ context.Context, d domain.Design, expectedSource string, expectedUpdatedAt time.Time, deleteAfter time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.byID[d.ID]
	if !ok || cur.SourceAssetID != expectedSource || !cur.UpdatedAt.Equal(expectedUpdatedAt) || !(domain.Design{Status: cur.Status}).EditableForPatch() {
		return ports.ErrDesignConflict
	}
	cur.Texts = d.Texts
	cur.SourceAssetID = d.SourceAssetID
	cur.Status = d.Status
	cur.UpdatedAt = d.UpdatedAt
	cur.ExpiresAt = d.ExpiresAt
	m.byID[d.ID] = cur
	if m.assets != nil {
		if a, ok := m.assets.byID[d.SourceAssetID]; ok && a.DeletedAt == nil {
			a.DeleteAfter = deleteAfter
			m.assets.byID[d.SourceAssetID] = a
		}
	}
	return nil
}
func (m *dDesignRepo) CreateBound(ctx context.Context, d domain.Design, deleteAfter time.Time) error {
	if err := m.Insert(ctx, d); err != nil {
		return err
	}
	return m.assets.BindOwnerRef(ctx, d.SourceAssetID, d.ID, deleteAfter)
}
func (m *dDesignRepo) ReplaceSource(ctx context.Context, d domain.Design, old string, expectedUpdatedAt time.Time, deleteAfter time.Time) (string, error) {
	oldKey := ""
	alreadyDeleted := false
	if old != "" && old != d.SourceAssetID {
		m.assets.mu.Lock()
		if a, ok := m.assets.byID[old]; ok {
			oldKey = a.StorageKey
			alreadyDeleted = a.DeletedAt != nil
		}
		m.assets.mu.Unlock()
		_ = m.assets.SoftDelete(ctx, old, d.UpdatedAt)
	}
	undoSoftDelete := func() {
		if alreadyDeleted || old == "" || old == d.SourceAssetID {
			return
		}
		m.assets.mu.Lock()
		if a, ok := m.assets.byID[old]; ok {
			a.DeletedAt = nil
			m.assets.byID[old] = a
		}
		m.assets.mu.Unlock()
	}
	if err := m.assets.BindOwnerRef(ctx, d.SourceAssetID, d.ID, deleteAfter); err != nil {
		undoSoftDelete()
		return "", err
	}
	if err := m.UpdateEditable(ctx, d, old, expectedUpdatedAt, deleteAfter); err != nil {
		_ = m.assets.ClearOwnerRef(ctx, d.SourceAssetID, d.ID)
		undoSoftDelete()
		return "", err
	}
	return oldKey, nil
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
func (m *dBlob) Read(_ context.Context, key string) ([]byte, error) {
	b, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("missing")
	}
	return append([]byte(nil), b...), nil
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
	if created.GenerationAttempts != 0 || created.GenerationAttemptsRemaining != 3 || created.MaxGenerationAttempts != 3 {
		t.Fatalf("attempt budget: %+v", created)
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

type failingJWTParser struct{ last string }

func (p *failingJWTParser) Parse(_ context.Context, token string) (identity.Identity, error) {
	p.last = token
	return identity.Identity{}, errors.New("jwt: malformed token")
}

func TestDesigns_AuthMiddlewareAllowsOwnerBearer(t *testing.T) {
	t.Parallel()
	tplID := "aaaaaaaa-bbbb-4ccc-8ddd-555555555555"
	uploadID := "aaaaaaaa-bbbb-4ccc-8ddd-666666666666"
	svc, _ := designSvc(t, tplID, uploadID, domain.TemplatePublished)

	mux := http.NewServeMux()
	mux.Handle("POST /api/customizer/designs", httpapi.CreateDesign(svc))
	mux.Handle("GET /api/customizer/designs/{id}", httpapi.GetDesign(svc))
	mux.Handle("PATCH /api/customizer/designs/{id}", httpapi.PatchDesign(svc))
	parser := &failingJWTParser{}
	handler := storefront.AuthMiddleware(parser)(mux)

	body, _ := json.Marshal(map[string]any{
		"template_id": tplID, "upload_id": uploadID, "texts": map[string]string{"dog_name": "Burek"},
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/customizer/designs", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var created httpapi.DesignResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.OwnerToken == "" {
		t.Fatal("missing owner_token")
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/customizer/designs/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+created.OwnerToken)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get through AuthMiddleware %d %s (parser last=%q)", rec.Code, rec.Body.String(), parser.last)
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["error"] != nil {
		t.Fatalf("want design JSON, got %+v", got)
	}
	if parser.last != "" {
		t.Fatalf("JWT parser must not see owner token, got %q", parser.last)
	}

	patch, _ := json.Marshal(map[string]any{"texts": map[string]string{"dog_name": "Azor"}})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/customizer/designs/"+created.ID, bytes.NewReader(patch))
	req.Header.Set("Authorization", "bearer "+created.OwnerToken) // case-insensitive scheme
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch %d %s", rec.Code, rec.Body.String())
	}
	var patchResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &patchResp)
	if patchResp["error"] != nil {
		t.Fatalf("shopanda error shape: %+v", patchResp)
	}
}

func TestDesigns_Create_BadUUIDsAndMalformedJSON(t *testing.T) {
	t.Parallel()
	tplID := "aaaaaaaa-bbbb-4ccc-8ddd-777777777777"
	uploadID := "aaaaaaaa-bbbb-4ccc-8ddd-888888888888"
	svc, _ := designSvc(t, tplID, uploadID, domain.TemplatePublished)

	rec := httptest.NewRecorder()
	httpapi.CreateDesign(svc).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/customizer/designs", bytes.NewReader([]byte(`{`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed json %d", rec.Code)
	}

	body, _ := json.Marshal(map[string]any{
		"template_id": "not-a-uuid", "upload_id": uploadID, "texts": map[string]string{"dog_name": "Burek"},
	})
	rec = httptest.NewRecorder()
	httpapi.CreateDesign(svc).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/customizer/designs", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad template_id %d %s", rec.Code, rec.Body.String())
	}
}

func TestDesigns_Patch_InvalidTransitionAndUploadAlreadyAttached(t *testing.T) {
	t.Parallel()
	tplID := "aaaaaaaa-bbbb-4ccc-8ddd-999999999991"
	uploadID := "aaaaaaaa-bbbb-4ccc-8ddd-999999999992"
	svc, key := designSvc(t, tplID, uploadID, domain.TemplatePublished)
	assets := svc.Assets.(*dAssetRepo)
	designs := svc.Designs.(*dDesignRepo)

	mux := http.NewServeMux()
	mux.Handle("POST /api/customizer/designs", httpapi.CreateDesign(svc))
	mux.Handle("GET /api/customizer/designs/{id}", httpapi.GetDesign(svc))
	mux.Handle("PATCH /api/customizer/designs/{id}", httpapi.PatchDesign(svc))

	body, _ := json.Marshal(map[string]any{
		"template_id": tplID, "upload_id": uploadID, "texts": map[string]string{"dog_name": "Burek"},
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/customizer/designs", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var created httpapi.DesignResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.OwnerToken == "" {
		t.Fatalf("create body: %v %+v", err, created)
	}

	designs.mu.Lock()
	d := designs.byID[created.ID]
	d.Status = domain.DesignGenerating
	designs.byID[created.ID] = d
	designs.mu.Unlock()

	patch, _ := json.Marshal(map[string]any{"texts": map[string]string{"dog_name": "Azor"}})
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/customizer/designs/"+created.ID, bytes.NewReader(patch))
	req.Header.Set("Authorization", "Bearer "+created.OwnerToken)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409 got %d %s", rec.Code, rec.Body.String())
	}
	assertErrCode(t, rec, application.DesignErrInvalidTransition)

	designs.mu.Lock()
	d = designs.byID[created.ID]
	d.Status = domain.DesignDraft
	designs.byID[created.ID] = d
	designs.mu.Unlock()
	other := "aaaaaaaa-bbbb-4ccc-8ddd-999999999993"
	assets.mu.Lock()
	assets.byID[other] = domain.Asset{
		ID: other, Type: domain.AssetTypeSource, StorageKey: key + ".2",
		MimeType: "image/jpeg", SizeBytes: 1, Width: 300, Height: 300, Checksum: "sha256:y",
		OwnerRef: "other-design", DeleteAfter: time.Now().Add(time.Hour),
	}
	assets.mu.Unlock()
	patch, _ = json.Marshal(map[string]any{"upload_id": other})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/customizer/designs/"+created.ID, bytes.NewReader(patch))
	req.Header.Set("Authorization", "Bearer "+created.OwnerToken)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 got %d %s", rec.Code, rec.Body.String())
	}
	assertErrCode(t, rec, application.DesignErrUploadAlreadyAttached)
}

type dJobRepo struct {
	byID map[string]domain.GenerationJob
}

func (r *dJobRepo) Insert(context.Context, domain.GenerationJob) error { return nil }
func (r *dJobRepo) GetByID(_ context.Context, id string) (domain.GenerationJob, bool, error) {
	j, ok := r.byID[id]
	return j, ok, nil
}
func (r *dJobRepo) GetByIdempotency(context.Context, string, string) (domain.GenerationJob, bool, error) {
	return domain.GenerationJob{}, false, nil
}
func (r *dJobRepo) GetActiveByDesign(context.Context, string) (domain.GenerationJob, bool, error) {
	return domain.GenerationJob{}, false, nil
}
func (r *dJobRepo) BeginGenerating(context.Context, domain.GenerationJob, domain.Design, time.Time) error {
	return nil
}
func (r *dJobRepo) ClaimForWork(context.Context, string, time.Duration) (domain.GenerationJob, error) {
	return domain.GenerationJob{}, nil
}
func (r *dJobRepo) ClaimLeaseExpired(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}
func (r *dJobRepo) InvalidateClaim(context.Context, string) error              { return nil }
func (r *dJobRepo) Update(context.Context, domain.GenerationJob, string) error { return nil }
func (r *dJobRepo) PersistArtwork(context.Context, domain.GenerationJob, domain.Asset) ([]ports.DeletedBlobRef, error) {
	return nil, nil
}
func (r *dJobRepo) CompleteSuccess(context.Context, domain.GenerationJob, domain.Asset, domain.Design) ([]ports.DeletedBlobRef, error) {
	return nil, nil
}
func (r *dJobRepo) FinalizeSuccess(context.Context, domain.GenerationJob, domain.Design, domain.Asset) ([]ports.DeletedBlobRef, error) {
	return nil, nil
}
func (r *dJobRepo) CompleteFailure(context.Context, domain.GenerationJob, domain.Design) error {
	return nil
}
func (r *dJobRepo) FailActive(context.Context, string, string, string, string, int64, time.Time) error {
	return nil
}

type dVersionRepo struct {
	mu       sync.Mutex
	byID     map[string]domain.DesignVersion
	byDesign map[string]string
	designs  *dDesignRepo
	assets   *dAssetRepo
}

func (r *dVersionRepo) GetByID(_ context.Context, id string) (domain.DesignVersion, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.byID[id]
	return v, ok, nil
}
func (r *dVersionRepo) GetByDesignID(_ context.Context, designID string) (domain.DesignVersion, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byDesign[designID]
	if !ok {
		return domain.DesignVersion{}, false, nil
	}
	return r.byID[id], true, nil
}
func (r *dVersionRepo) ApproveBound(_ context.Context, d domain.Design, v domain.DesignVersion) error {
	if d.ID == "" || v.DesignID != d.ID || d.ApprovedVersionID != v.ID {
		return fmt.Errorf("id mismatch")
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
			art.OwnerRef != d.ID || art.Checksum != v.ArtworkChecksum {
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
	r.byID[v.ID] = v
	r.byDesign[v.DesignID] = v.ID
	r.mu.Unlock()
	r.designs.mu.Lock()
	defer r.designs.mu.Unlock()
	cur = r.designs.byID[d.ID]
	if cur.Status != domain.DesignPreviewReady {
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
	return nil
}

func TestDesigns_ApproveAndClone(t *testing.T) {
	t.Parallel()
	tplID := "aaaaaaaa-bbbb-4ccc-8ddd-aaaa11111111"
	uploadID := "aaaaaaaa-bbbb-4ccc-8ddd-aaaa22222222"
	artID := "aaaaaaaa-bbbb-4ccc-8ddd-aaaa33333333"
	jobID := "aaaaaaaa-bbbb-4ccc-8ddd-aaaa55555555"
	svc, key := designSvc(t, tplID, uploadID, domain.TemplatePublished)
	assets := svc.Assets.(*dAssetRepo)
	designs := svc.Designs.(*dDesignRepo)
	store := svc.Store.(*dBlob)
	// Replace create stub bytes with sniffable JPEG + matching metadata.
	store.objects[key] = append([]byte(nil), httpJPEG...)
	assets.mu.Lock()
	src := assets.byID[uploadID]
	src.SizeBytes = int64(len(httpJPEG))
	src.Checksum = httpSHA(httpJPEG)
	assets.byID[uploadID] = src
	assets.mu.Unlock()

	jobs := &dJobRepo{byID: map[string]domain.GenerationJob{}}
	versions := &dVersionRepo{designs: designs, assets: assets}
	svc.Jobs = jobs
	svc.Versions = versions
	svc.EnqueuePrepare = func(context.Context, string, string) error { return nil }
	svc.IDs = ports.UUIDGenerator{}

	mux := http.NewServeMux()
	mux.Handle("POST /api/customizer/designs", httpapi.CreateDesign(svc))
	mux.Handle("PATCH /api/customizer/designs/{id}", httpapi.PatchDesign(svc))
	mux.Handle("POST /api/customizer/designs/{id}/approve", httpapi.ApproveDesign(svc))
	mux.Handle("POST /api/customizer/designs/{id}/clone", httpapi.CloneDesign(svc))

	body, _ := json.Marshal(map[string]any{
		"template_id": tplID, "upload_id": uploadID, "texts": map[string]string{"dog_name": "Burek"},
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

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/customizer/designs/"+created.ID+"/approve", nil)
	req.Header.Set("Authorization", "Bearer "+created.OwnerToken)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("draft approve want 409 got %d %s", rec.Code, rec.Body.String())
	}
	assertErrCode(t, rec, application.DesignErrInvalidTransition)

	designs.mu.Lock()
	d := designs.byID[created.ID]
	d.Status = domain.DesignPreviewReady
	d.LatestJobID = jobID
	designs.byID[created.ID] = d
	designs.mu.Unlock()
	artKey := key + ".art"
	store.objects[artKey] = append([]byte(nil), httpPNG...)
	assets.mu.Lock()
	assets.byID[artID] = domain.Asset{
		ID: artID, Type: domain.AssetTypeArtwork, OwnerRef: created.ID, StorageKey: artKey,
		MimeType: "image/png", SizeBytes: int64(len(httpPNG)), Width: 64, Height: 64,
		Checksum: httpSHA(httpPNG), DeleteAfter: time.Now().Add(time.Hour),
	}
	assets.mu.Unlock()
	jobs.byID[jobID] = domain.GenerationJob{
		ID: jobID, DesignID: created.ID, Status: domain.JobSucceeded,
		ArtworkAssetID: artID, Provider: "fake", Model: "m", PromptVersion: "pv1",
		ParametersJSON: []byte(`{}`),
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/customizer/designs/"+created.ID+"/approve", nil)
	req.Header.Set("Authorization", "Bearer "+created.OwnerToken)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve %d %s", rec.Code, rec.Body.String())
	}
	var approved httpapi.ApproveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &approved); err != nil {
		t.Fatal(err)
	}
	if approved.Design.Status != domain.DesignApproved || approved.Version.ID == "" || approved.Version.ArtworkAssetID != artID {
		t.Fatalf("%+v", approved)
	}
	verID := approved.Version.ID

	patch, _ := json.Marshal(map[string]any{"texts": map[string]string{"dog_name": "Azor"}})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/customizer/designs/"+created.ID, bytes.NewReader(patch))
	req.Header.Set("Authorization", "Bearer "+created.OwnerToken)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("patch approved want 409 got %d %s", rec.Code, rec.Body.String())
	}
	assertErrCode(t, rec, application.DesignErrInvalidTransition)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/customizer/designs/"+created.ID+"/approve", nil)
	req.Header.Set("Authorization", "Bearer "+created.OwnerToken)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent approve %d %s", rec.Code, rec.Body.String())
	}
	var again httpapi.ApproveResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &again)
	if again.Version.ID != verID {
		t.Fatalf("version %q want %q", again.Version.ID, verID)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/customizer/designs/"+created.ID+"/clone", nil)
	req.Header.Set("Authorization", "Bearer "+created.OwnerToken)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("clone %d %s", rec.Code, rec.Body.String())
	}
	var cloned httpapi.DesignResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &cloned); err != nil {
		t.Fatal(err)
	}
	if cloned.ID == created.ID || cloned.OwnerToken == "" || cloned.Status != domain.DesignDraft {
		t.Fatalf("%+v", cloned)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/customizer/designs/"+created.ID+"/clone", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("no bearer clone want 404 got %d", rec.Code)
	}
}

func TestDesigns_Approve_EnqueueFail503(t *testing.T) {
	t.Parallel()
	tplID := "aaaaaaaa-bbbb-4ccc-8ddd-bbbb11111111"
	uploadID := "aaaaaaaa-bbbb-4ccc-8ddd-bbbb22222222"
	artID := "aaaaaaaa-bbbb-4ccc-8ddd-bbbb33333333"
	jobID := "aaaaaaaa-bbbb-4ccc-8ddd-bbbb55555555"
	svc, key := designSvc(t, tplID, uploadID, domain.TemplatePublished)
	assets := svc.Assets.(*dAssetRepo)
	designs := svc.Designs.(*dDesignRepo)
	store := svc.Store.(*dBlob)
	store.objects[key] = append([]byte(nil), httpJPEG...)
	assets.mu.Lock()
	src := assets.byID[uploadID]
	src.SizeBytes = int64(len(httpJPEG))
	src.Checksum = httpSHA(httpJPEG)
	assets.byID[uploadID] = src
	assets.mu.Unlock()
	jobs := &dJobRepo{byID: map[string]domain.GenerationJob{}}
	versions := &dVersionRepo{designs: designs, assets: assets}
	svc.Jobs, svc.Versions = jobs, versions
	svc.EnqueuePrepare = func(context.Context, string, string) error { return errors.New("queue down") }
	svc.IDs = ports.UUIDGenerator{}

	mux := http.NewServeMux()
	mux.Handle("POST /api/customizer/designs", httpapi.CreateDesign(svc))
	mux.Handle("POST /api/customizer/designs/{id}/approve", httpapi.ApproveDesign(svc))

	body, _ := json.Marshal(map[string]any{
		"template_id": tplID, "upload_id": uploadID, "texts": map[string]string{"dog_name": "Burek"},
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/customizer/designs", bytes.NewReader(body)))
	var created httpapi.DesignResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	designs.mu.Lock()
	d := designs.byID[created.ID]
	d.Status = domain.DesignPreviewReady
	d.LatestJobID = jobID
	designs.byID[created.ID] = d
	designs.mu.Unlock()
	artKey := key + ".art"
	store.objects[artKey] = append([]byte(nil), httpPNG...)
	assets.mu.Lock()
	assets.byID[artID] = domain.Asset{
		ID: artID, Type: domain.AssetTypeArtwork, OwnerRef: created.ID, StorageKey: artKey,
		MimeType: "image/png", SizeBytes: int64(len(httpPNG)), Checksum: httpSHA(httpPNG),
		DeleteAfter: time.Now().Add(time.Hour),
	}
	assets.mu.Unlock()
	jobs.byID[jobID] = domain.GenerationJob{
		ID: jobID, DesignID: created.ID, Status: domain.JobSucceeded, ArtworkAssetID: artID,
		Provider: "fake", Model: "m", PromptVersion: "pv1", ParametersJSON: []byte(`{}`),
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/customizer/designs/"+created.ID+"/approve", nil)
	req.Header.Set("Authorization", "Bearer "+created.OwnerToken)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d %s", rec.Code, rec.Body.String())
	}
	var errBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if errBody["error"] != application.DesignErrUnavailable || errBody["version_id"] == nil || errBody["version_id"] == "" {
		t.Fatalf("body=%+v", errBody)
	}
}
