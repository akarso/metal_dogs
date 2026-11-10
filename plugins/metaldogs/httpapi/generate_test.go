package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

// Reuse lightweight fakes from designs_test via duplicated minimal wiring.

type gJobRepo struct {
	job domain.GenerationJob
}

func (g *gJobRepo) Insert(context.Context, domain.GenerationJob) error { return nil }
func (g *gJobRepo) GetByID(_ context.Context, id string) (domain.GenerationJob, bool, error) {
	if g.job.ID == id {
		return g.job, true, nil
	}
	return domain.GenerationJob{}, false, nil
}
func (g *gJobRepo) GetByIdempotency(_ context.Context, designID, key string) (domain.GenerationJob, bool, error) {
	if g.job.DesignID == designID && g.job.IdempotencyKey == key {
		return g.job, true, nil
	}
	return domain.GenerationJob{}, false, nil
}
func (g *gJobRepo) GetActiveByDesign(context.Context, string) (domain.GenerationJob, bool, error) {
	return domain.GenerationJob{}, false, nil
}
func (g *gJobRepo) BeginGenerating(_ context.Context, j domain.GenerationJob, _ domain.Design, _ time.Time) error {
	g.job = j
	return nil
}
func (g *gJobRepo) ClaimForWork(_ context.Context, id string, lease time.Duration) (domain.GenerationJob, error) {
	if g.job.ID != id {
		return domain.GenerationJob{}, ports.ErrGenerationJobStale
	}
	j := g.job
	if err := j.ClaimForWork(time.Now().UTC(), lease); err != nil {
		return domain.GenerationJob{}, ports.ErrGenerationJobStale
	}
	g.job = j
	return j, nil
}
func (g *gJobRepo) ClaimLeaseExpired(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}
func (g *gJobRepo) InvalidateClaim(context.Context, string) error { return nil }
func (g *gJobRepo) Update(_ context.Context, j domain.GenerationJob, _ string) error {
	g.job = j
	return nil
}
func (g *gJobRepo) PersistArtwork(context.Context, domain.GenerationJob, domain.Asset) ([]ports.DeletedBlobRef, error) {
	return nil, nil
}
func (g *gJobRepo) CompleteSuccess(context.Context, domain.GenerationJob, domain.Asset, domain.Design) ([]ports.DeletedBlobRef, error) {
	return nil, nil
}
func (g *gJobRepo) FinalizeSuccess(context.Context, domain.GenerationJob, domain.Design, domain.Asset) ([]ports.DeletedBlobRef, error) {
	return nil, nil
}
func (g *gJobRepo) CompleteFailure(context.Context, domain.GenerationJob, domain.Design) error {
	return nil
}
func (g *gJobRepo) FailActive(context.Context, string, string, string, string, int64, time.Time) error {
	return nil
}

var _ ports.GenerationJobRepository = (*gJobRepo)(nil)

func TestGenerateDesign_HTTP(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	designID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	jobID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	uploadID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	token, hash, err := application.MintOwnerToken()
	if err != nil {
		t.Fatal(err)
	}
	designs := &dDesignRepo{byID: map[string]domain.Design{
		designID: {
			ID: designID, Status: domain.DesignDraft, SourceAssetID: uploadID,
			OwnerTokenHash: hash, Texts: map[string]string{"dog_name": "Burek"},
			ExpiresAt: now.Add(time.Hour), UpdatedAt: now, CreatedAt: now,
			TemplateID: "t1", TemplateVersion: 1,
		},
	}}
	jobs := &gJobRepo{}
	ids := &testutil.FixedGenerator{ID: jobID}
	svc := &application.GenerateService{
		Designs: designs, Jobs: jobs, Assets: &dAssetRepo{byID: map[string]domain.Asset{
			uploadID: {
				ID: uploadID, Type: domain.AssetTypeSource,
				StorageKey: "private/blobs/metaldogs/source/2026/11/" + uploadID + ".jpg",
				MimeType:   "image/jpeg", SizeBytes: 10, Width: 64, Height: 64, Checksum: "sha256:x",
				DeleteAfter: now.Add(time.Hour), OwnerRef: designID,
			},
		}},
		IDs: ids, Clock: testutil.NewFakeClock(now),
		Enqueue:  func(context.Context, string, string) error { return nil },
		Provider: "fake", MaxAttempts: 3,
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/customizer/designs/{id}/generate", httpapi.GenerateDesign(svc))
	mux.Handle("GET /api/customizer/generation-jobs/{id}", httpapi.GetGenerationJob(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/customizer/designs/"+designID+"/generate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", "idem-http-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body httpapi.GenerateAccepted
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.JobID == "" || body.Status != domain.JobQueued || body.RetryAfter != 2 {
		t.Fatalf("%+v", body)
	}

	// Idempotent replay
	req2 := httptest.NewRequest(http.MethodPost, "/api/customizer/designs/"+designID+"/generate", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Idempotency-Key", "idem-http-1")
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusAccepted {
		t.Fatalf("replay status=%d", rec2.Code)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/customizer/generation-jobs/"+body.JobID, nil)
	get.Header.Set("Authorization", "Bearer "+token)
	grec := httptest.NewRecorder()
	mux.ServeHTTP(grec, get)
	if grec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", grec.Code, grec.Body.String())
	}

	// Missing idempotency
	req3 := httptest.NewRequest(http.MethodPost, "/api/customizer/designs/"+designID+"/generate", nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest || !strings.Contains(rec3.Body.String(), application.GenerateErrBadRequest) {
		t.Fatalf("missing key: %d %s", rec3.Code, rec3.Body.String())
	}
}

func TestGenerateDesign_NilService(t *testing.T) {
	t.Parallel()
	h := httpapi.GenerateDesign(nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestGenerateDesign_EnqueueUnavailableIncludesJobID(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	designID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	jobID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	uploadID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	token, hash, err := application.MintOwnerToken()
	if err != nil {
		t.Fatal(err)
	}
	svc := &application.GenerateService{
		Designs: &dDesignRepo{byID: map[string]domain.Design{
			designID: {
				ID: designID, Status: domain.DesignDraft, SourceAssetID: uploadID,
				OwnerTokenHash: hash, Texts: map[string]string{"dog_name": "Burek"},
				ExpiresAt: now.Add(time.Hour), UpdatedAt: now, CreatedAt: now,
				TemplateID: "t1", TemplateVersion: 1,
			},
		}},
		Jobs: &gJobRepo{},
		Assets: &dAssetRepo{byID: map[string]domain.Asset{
			uploadID: {
				ID: uploadID, Type: domain.AssetTypeSource,
				StorageKey: "private/blobs/metaldogs/source/2026/11/" + uploadID + ".jpg",
				MimeType:   "image/jpeg", SizeBytes: 10, Width: 64, Height: 64, Checksum: "sha256:x",
				DeleteAfter: now.Add(time.Hour), OwnerRef: designID,
			},
		}},
		IDs: &testutil.FixedGenerator{ID: jobID}, Clock: testutil.NewFakeClock(now),
		Enqueue:  func(context.Context, string, string) error { return context.DeadlineExceeded },
		Provider: "fake", MaxAttempts: 3,
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/customizer/designs/{id}/generate", httpapi.GenerateDesign(svc))
	req := httptest.NewRequest(http.MethodPost, "/api/customizer/designs/"+designID+"/generate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", "idem-503")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != application.GenerateErrUnavailable || body["job_id"] != jobID {
		t.Fatalf("%+v", body)
	}
}
