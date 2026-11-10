package application_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	platformjobs "github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

type genJobRepo struct {
	mu      sync.Mutex
	byID    map[string]domain.GenerationJob
	byIdem  map[string]string
	active  map[string]string
	designs *designMemRepo
	assets  *designAssetRepo
}

func newGenJobRepo(designs *designMemRepo, assets *designAssetRepo) *genJobRepo {
	return &genJobRepo{
		byID: map[string]domain.GenerationJob{}, byIdem: map[string]string{}, active: map[string]string{},
		designs: designs, assets: assets,
	}
}

func idemKey(designID, key string) string { return designID + "\x00" + key }

func (m *genJobRepo) insertLocked(j domain.GenerationJob) error {
	if j.IsActive() {
		if _, ok := m.active[j.DesignID]; ok {
			return ports.ErrGenerationJobConflict
		}
	}
	if j.IdempotencyKey != "" {
		if _, ok := m.byIdem[idemKey(j.DesignID, j.IdempotencyKey)]; ok {
			return ports.ErrGenerationJobConflict
		}
		m.byIdem[idemKey(j.DesignID, j.IdempotencyKey)] = j.ID
	}
	m.byID[j.ID] = j
	if j.IsActive() {
		m.active[j.DesignID] = j.ID
	}
	return nil
}

func (m *genJobRepo) Insert(_ context.Context, j domain.GenerationJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.insertLocked(j)
}

func (m *genJobRepo) GetByID(_ context.Context, id string) (domain.GenerationJob, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.byID[id]
	return j, ok, nil
}

func (m *genJobRepo) GetByIdempotency(_ context.Context, designID, key string) (domain.GenerationJob, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byIdem[idemKey(designID, key)]
	if !ok {
		return domain.GenerationJob{}, false, nil
	}
	j, ok := m.byID[id]
	return j, ok, nil
}

func (m *genJobRepo) GetActiveByDesign(_ context.Context, designID string) (domain.GenerationJob, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.active[designID]
	if !ok {
		return domain.GenerationJob{}, false, nil
	}
	j, ok := m.byID[id]
	return j, ok, nil
}

func (m *genJobRepo) BeginGenerating(_ context.Context, j domain.GenerationJob, d domain.Design, sourceDeleteAfter time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.insertLocked(j); err != nil {
		return err
	}
	m.designs.mu.Lock()
	defer m.designs.mu.Unlock()
	cur, ok := m.designs.byID[d.ID]
	if !ok || !cur.CanRequestGeneration() {
		return ports.ErrDesignConflict
	}
	cur.Status = d.Status
	cur.GenerationAttempts = d.GenerationAttempts
	cur.LatestJobID = d.LatestJobID
	cur.UpdatedAt = d.UpdatedAt
	cur.ExpiresAt = d.ExpiresAt
	m.designs.byID[d.ID] = cur
	if m.assets != nil && d.SourceAssetID != "" && !sourceDeleteAfter.IsZero() {
		m.assets.mu.Lock()
		a, ok := m.assets.byID[d.SourceAssetID]
		if !ok || a.DeletedAt != nil || a.Type != domain.AssetTypeSource {
			m.assets.mu.Unlock()
			return ports.ErrDesignConflict
		}
		a.DeleteAfter = sourceDeleteAfter
		m.assets.byID[d.SourceAssetID] = a
		m.assets.mu.Unlock()
	}
	return nil
}

func (m *genJobRepo) ClaimForWork(_ context.Context, id string, lease time.Duration) (domain.GenerationJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.byID[id]
	if !ok {
		return domain.GenerationJob{}, ports.ErrGenerationJobStale
	}
	if err := cur.ClaimForWork(time.Now().UTC(), lease); err != nil {
		if errors.Is(err, domain.ErrClaimHeld) {
			return domain.GenerationJob{}, ports.ErrGenerationJobStale
		}
		return domain.GenerationJob{}, ports.ErrGenerationJobStale
	}
	m.byID[id] = cur
	m.active[cur.DesignID] = cur.ID
	return cur, nil
}

func (m *genJobRepo) ClaimLeaseExpired(_ context.Context, id string, lease time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.byID[id]
	if !ok {
		return false, nil
	}
	return cur.LeaseExpired(time.Now().UTC(), lease), nil
}

func (m *genJobRepo) InvalidateClaim(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.byID[id]
	if !ok {
		return nil
	}
	cur.ClaimToken++
	cur.StartedAt = nil
	m.byID[id] = cur
	return nil
}

func (m *genJobRepo) Update(_ context.Context, j domain.GenerationJob, expectedStatus string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.byID[j.ID]
	if !ok {
		return ports.ErrGenerationJobStale
	}
	if expectedStatus != "" && cur.Status != expectedStatus {
		return ports.ErrGenerationJobStale
	}
	if cur.ClaimToken != j.ClaimToken {
		return ports.ErrGenerationJobStale
	}
	m.byID[j.ID] = j
	if j.IsActive() {
		m.active[j.DesignID] = j.ID
	} else if m.active[j.DesignID] == j.ID {
		delete(m.active, j.DesignID)
	}
	return nil
}

func (m *genJobRepo) CompleteSuccess(_ context.Context, j domain.GenerationJob, artwork domain.Asset, design domain.Design) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	curJob, ok := m.byID[j.ID]
	if !ok || !curJob.IsActive() || curJob.ClaimToken != j.ClaimToken {
		return ports.ErrGenerationJobStale
	}
	if m.assets != nil {
		m.assets.mu.Lock()
		for id, a := range m.assets.byID {
			if a.OwnerRef == artwork.OwnerRef && a.Type == domain.AssetTypeArtwork && a.DeletedAt == nil {
				now := design.UpdatedAt
				a.DeletedAt = &now
				m.assets.byID[id] = a
			}
		}
		m.assets.mu.Unlock()
		_ = m.assets.Insert(context.Background(), artwork)
	}
	m.byID[j.ID] = j
	delete(m.active, j.DesignID)
	m.designs.mu.Lock()
	defer m.designs.mu.Unlock()
	cur, ok := m.designs.byID[design.ID]
	if !ok || cur.Status != domain.DesignGenerating {
		return ports.ErrDesignConflict
	}
	cur.Status = design.Status
	cur.UpdatedAt = design.UpdatedAt
	m.designs.byID[design.ID] = cur
	return nil
}

func (m *genJobRepo) FinalizeSuccess(_ context.Context, j domain.GenerationJob, design domain.Design) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	curJob, ok := m.byID[j.ID]
	if !ok || !curJob.IsActive() || curJob.ClaimToken != j.ClaimToken {
		return ports.ErrGenerationJobStale
	}
	m.byID[j.ID] = j
	delete(m.active, j.DesignID)
	m.designs.mu.Lock()
	defer m.designs.mu.Unlock()
	cur, ok := m.designs.byID[design.ID]
	if !ok {
		return nil
	}
	if cur.Status == domain.DesignGenerating || cur.Status == domain.DesignPreviewReady {
		cur.Status = domain.DesignPreviewReady
		cur.UpdatedAt = design.UpdatedAt
		m.designs.byID[design.ID] = cur
	}
	return nil
}

func (m *genJobRepo) CompleteFailure(_ context.Context, j domain.GenerationJob, design domain.Design) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.byID[j.ID]
	if !ok || !cur.IsActive() || cur.ClaimToken != j.ClaimToken {
		return ports.ErrGenerationJobStale
	}
	m.byID[j.ID] = j
	delete(m.active, j.DesignID)
	m.designs.mu.Lock()
	defer m.designs.mu.Unlock()
	d, ok := m.designs.byID[design.ID]
	if ok && d.Status == domain.DesignGenerating {
		d.Status = domain.DesignGenerationFailed
		d.UpdatedAt = design.UpdatedAt
		m.designs.byID[design.ID] = d
	}
	return nil
}

func (m *genJobRepo) FailActive(_ context.Context, id, designID, terminalReason, errorCode string, claimToken int64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.byID[id]
	if !ok || !cur.IsActive() || cur.ClaimToken != claimToken {
		return ports.ErrGenerationJobStale
	}
	if cur.ArtworkAssetID != "" {
		return ports.ErrGenerationJobHasArtwork
	}
	cur.Status = domain.JobFailed
	cur.TerminalReason = terminalReason
	cur.ErrorCode = errorCode
	now := at.UTC()
	cur.FinishedAt = &now
	m.byID[id] = cur
	delete(m.active, cur.DesignID)
	m.designs.mu.Lock()
	defer m.designs.mu.Unlock()
	if d, ok := m.designs.byID[designID]; ok && d.Status == domain.DesignGenerating {
		d.Status = domain.DesignGenerationFailed
		d.UpdatedAt = now
		m.designs.byID[designID] = d
	}
	return nil
}

var _ ports.GenerationJobRepository = (*genJobRepo)(nil)

type countingProvider struct {
	inner generation.ImageProvider
	mu    sync.Mutex
	calls int
}

func (c *countingProvider) Name() string { return c.inner.Name() }
func (c *countingProvider) Generate(ctx context.Context, req generation.GenerationRequest) (generation.GenerationResult, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.inner.Generate(ctx, req)
}

type rejectProvider struct{}

func (rejectProvider) Name() string { return "fake" }
func (rejectProvider) Generate(context.Context, generation.GenerationRequest) (generation.GenerationResult, error) {
	return generation.GenerationResult{}, &generation.ProviderError{Class: generation.ErrorRejectedInput, Message: "bad photo"}
}

type transientProvider struct {
	mu    sync.Mutex
	calls int
}

func (t *transientProvider) Name() string { return "fake" }
func (t *transientProvider) Generate(context.Context, generation.GenerationRequest) (generation.GenerationResult, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	return generation.GenerationResult{}, &generation.ProviderError{Class: generation.ErrorTransient, Message: "timeout"}
}

func TestGenerateService_RequestAndIdempotency(t *testing.T) {
	t.Parallel()
	svc, _, designs, jobs, assets, _, _, designID, token, _ := genFixture(t)
	view, err := svc.RequestGenerate(context.Background(), designID, token, "key-1", "corr")
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != domain.JobQueued || view.RetryAfter != 2 {
		t.Fatalf("%+v", view)
	}
	d, ok, _ := designs.GetByID(context.Background(), designID)
	if !ok || d.Status != domain.DesignGenerating || d.GenerationAttempts != 1 {
		t.Fatalf("%+v", d)
	}
	src, _, _ := assets.GetByID(context.Background(), d.SourceAssetID)
	if !src.DeleteAfter.After(time.Now().UTC()) {
		t.Fatalf("source delete_after not slid: %v", src.DeleteAfter)
	}
	again, err := svc.RequestGenerate(context.Background(), designID, token, "key-1", "corr")
	if err != nil || again.ID != view.ID {
		t.Fatalf("idempotency: %v %+v", err, again)
	}
	_, err = svc.RequestGenerate(context.Background(), designID, token, "key-2", "")
	var ge *application.GenerateError
	if err == nil || !errors.As(err, &ge) || ge.Code != application.GenerateErrInvalidTransition || ge.JobID == "" {
		t.Fatalf("second key while generating: err=%v", err)
	}
	_ = jobs
}

func TestGenerateService_EnqueueFailureThenReplay(t *testing.T) {
	t.Parallel()
	svc, _, _, jobs, _, _, _, designID, token, _ := genFixture(t)
	var calls int
	svc.Enqueue = func(context.Context, string, string) error {
		calls++
		if calls == 1 {
			return errors.New("queue down")
		}
		return nil
	}
	_, err := svc.RequestGenerate(context.Background(), designID, token, "enq-1", "")
	var ge *application.GenerateError
	if err == nil || !errors.As(err, &ge) || ge.Code != application.GenerateErrUnavailable || ge.JobID == "" {
		t.Fatalf("first: %v", err)
	}
	job, ok, _ := jobs.GetByID(context.Background(), ge.JobID)
	if !ok || job.Status != domain.JobQueued {
		t.Fatalf("%+v", job)
	}
	view, err := svc.RequestGenerate(context.Background(), designID, token, "enq-1", "")
	if err != nil || view.ID != ge.JobID || calls < 2 {
		t.Fatalf("replay: err=%v view=%+v calls=%d", err, view, calls)
	}
}

func TestGenerateService_MissingSource(t *testing.T) {
	t.Parallel()
	svc, _, designs, _, assets, _, _, designID, token, _ := genFixture(t)
	d, _, _ := designs.GetByID(context.Background(), designID)
	_ = assets.SoftDelete(context.Background(), d.SourceAssetID, time.Now().UTC())
	_, err := svc.RequestGenerate(context.Background(), designID, token, "k", "")
	var ge *application.GenerateError
	if err == nil || !errors.As(err, &ge) || ge.Code != application.GenerateErrUploadUnavailable {
		t.Fatalf("err=%v", err)
	}
}

func TestGenerateWorker_FakePreviewReady(t *testing.T) {
	t.Parallel()
	svc, worker, designs, jobs, assets, _, prov, designID, token, _ := genFixture(t)
	view, err := svc.RequestGenerate(context.Background(), designID, token, "run-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Process(context.Background(), view.ID, designID, 1); err != nil {
		t.Fatal(err)
	}
	job, ok, _ := jobs.GetByID(context.Background(), view.ID)
	if !ok || job.Status != domain.JobSucceeded || job.ArtworkAssetID == "" {
		t.Fatalf("%+v", job)
	}
	d, _, _ := designs.GetByID(context.Background(), designID)
	if d.Status != domain.DesignPreviewReady {
		t.Fatalf("design status=%s", d.Status)
	}
	art, ok, _ := assets.GetByID(context.Background(), job.ArtworkAssetID)
	if !ok || art.Type != domain.AssetTypeArtwork || art.OwnerRef != designID {
		t.Fatalf("%+v ok=%v", art, ok)
	}
	if prov.calls != 1 {
		t.Fatalf("provider calls=%d", prov.calls)
	}
}

type fenceAfterClaimRepo struct {
	*genJobRepo
	claims int
}

func (f *fenceAfterClaimRepo) ClaimForWork(ctx context.Context, id string, lease time.Duration) (domain.GenerationJob, error) {
	j, err := f.genJobRepo.ClaimForWork(ctx, id, lease)
	if err == nil {
		f.claims++
	}
	return j, err
}

func (f *fenceAfterClaimRepo) GetByID(ctx context.Context, id string) (domain.GenerationJob, bool, error) {
	j, ok, err := f.genJobRepo.GetByID(ctx, id)
	if err != nil || !ok {
		return j, ok, err
	}
	if f.claims > 0 {
		j.ClaimToken = j.ClaimToken + 100 // ensureClaimHeld sees a lost fence
	}
	return j, true, nil
}

func TestGenerateWorker_StageCancelWhileHoldingClaim_RequeuesProduct(t *testing.T) {
	t.Parallel()
	svc, worker, _, jobsRepo, _, _, prov, designID, token, _ := genFixture(t)
	view, err := svc.RequestGenerate(context.Background(), designID, token, "stage-cancel-1", "")
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	release := make(chan struct{})
	orig := prov.inner
	prov.inner = &gatedProvider{inner: orig, gate: gate, release: release}

	parent, cancelParent := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- worker.Process(parent, view.ID, designID, 1) }()
	<-gate // inside Generate; pre-check passed
	cancelParent()
	close(release)
	err = <-errCh
	if !errors.Is(err, application.ErrTransientGeneration) {
		t.Fatalf("want transient after stage cancel, got %v", err)
	}
	job, _, _ := jobsRepo.GetByID(context.Background(), view.ID)
	if job.Status != domain.JobQueued || job.StartedAt != nil {
		t.Fatalf("product must leave running for next claim, got %+v", job)
	}
}

func TestGenerateWorker_LostClaimBeforeGenerate_SkipsProvider(t *testing.T) {
	t.Parallel()
	svc, worker, _, jobsRepo, _, _, prov, designID, token, _ := genFixture(t)
	view, err := svc.RequestGenerate(context.Background(), designID, token, "lost-claim-1", "")
	if err != nil {
		t.Fatal(err)
	}
	worker.Jobs = &fenceAfterClaimRepo{genJobRepo: jobsRepo}
	err = worker.Process(context.Background(), view.ID, designID, 1)
	if !errors.Is(err, platformjobs.ErrSkipOutcome) {
		t.Fatalf("want ErrSkipOutcome, got %v", err)
	}
	if prov.calls != 0 {
		t.Fatalf("provider must not run after lost claim, calls=%d", prov.calls)
	}
}

func TestGenerateWorker_LostClaimAfterGenerate_SkipsComplete(t *testing.T) {
	t.Parallel()
	svc, worker, _, jobsRepo, _, _, prov, designID, token, _ := genFixture(t)
	view, err := svc.RequestGenerate(context.Background(), designID, token, "lost-mid-1", "")
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	release := make(chan struct{})
	orig := prov.inner
	prov.inner = &gatedProvider{inner: orig, gate: gate, release: release}

	errCh := make(chan error, 1)
	go func() { errCh <- worker.Process(context.Background(), view.ID, designID, 1) }()
	<-gate // passed pre-Generate claim check; inside provider
	if err := jobsRepo.InvalidateClaim(context.Background(), view.ID); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	err = <-errCh
	if !errors.Is(err, platformjobs.ErrSkipOutcome) {
		t.Fatalf("want ErrSkipOutcome after lost claim, got %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("provider calls=%d", prov.calls)
	}
	job, _, _ := jobsRepo.GetByID(context.Background(), view.ID)
	if job.Status == domain.JobSucceeded {
		t.Fatalf("lost claim must not CompleteSuccess: %+v", job)
	}
}

func TestGenerateWorker_ValidatingRedelivery_RecallsProvider(t *testing.T) {
	t.Parallel()
	svc, worker, _, jobs, _, _, prov, designID, token, _ := genFixture(t)
	view, err := svc.RequestGenerate(context.Background(), designID, token, "val-1", "")
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobs.ClaimForWork(context.Background(), view.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	job.Status = domain.JobValidating
	if err := jobs.Update(context.Background(), job, domain.JobRunning); err != nil {
		t.Fatal(err)
	}
	if err := worker.Process(context.Background(), view.ID, designID, 1); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("provider calls=%d want 1 after validating reclaim", prov.calls)
	}
	job, _, _ = jobs.GetByID(context.Background(), view.ID)
	if job.Status != domain.JobSucceeded {
		t.Fatalf("%+v", job)
	}
}

func TestGenerateWorker_LastAttemptStorageTerminal(t *testing.T) {
	t.Parallel()
	svc, worker, designs, jobs, _, store, _, designID, token, _ := genFixture(t)
	view, err := svc.RequestGenerate(context.Background(), designID, token, "last-1", "")
	if err != nil {
		t.Fatal(err)
	}
	// Break source blob so Read fails.
	d, _, _ := designs.GetByID(context.Background(), designID)
	delete(store.objects, "")
	for k := range store.objects {
		delete(store.objects, k)
	}
	_ = d
	err = worker.Process(context.Background(), view.ID, designID, 3) // last attempt
	if err != nil {
		t.Fatalf("last attempt should terminal nil, got %v", err)
	}
	job, _, _ := jobs.GetByID(context.Background(), view.ID)
	if job.Status != domain.JobFailed || job.TerminalReason != domain.TerminalStorageError {
		t.Fatalf("%+v", job)
	}
	d, _, _ = designs.GetByID(context.Background(), designID)
	if d.Status != domain.DesignGenerationFailed {
		t.Fatalf("status=%s", d.Status)
	}
}

func TestGenerateWorker_TransientThenExhaust(t *testing.T) {
	t.Parallel()
	svc, worker, designs, jobs, _, _, _, designID, token, _ := genFixture(t)
	tp := &transientProvider{}
	worker.Provider = tp
	view, err := svc.RequestGenerate(context.Background(), designID, token, "tr-1", "")
	if err != nil {
		t.Fatal(err)
	}
	err = worker.Process(context.Background(), view.ID, designID, 1)
	if !errors.Is(err, application.ErrTransientGeneration) {
		t.Fatalf("want transient, got %v", err)
	}
	err = worker.Process(context.Background(), view.ID, designID, 3)
	if err != nil {
		t.Fatalf("last: %v", err)
	}
	job, _, _ := jobs.GetByID(context.Background(), view.ID)
	if job.Status != domain.JobFailed {
		t.Fatalf("%+v", job)
	}
	d, _, _ := designs.GetByID(context.Background(), designID)
	if d.Status != domain.DesignGenerationFailed {
		t.Fatalf("%s", d.Status)
	}
}

func TestGenerateWorker_PermanentFailure(t *testing.T) {
	t.Parallel()
	svc, worker, designs, jobs, _, _, _, designID, token, _ := genFixture(t)
	worker.Provider = rejectProvider{}
	view, err := svc.RequestGenerate(context.Background(), designID, token, "fail-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Process(context.Background(), view.ID, designID, 1); err != nil {
		t.Fatal(err)
	}
	job, _, _ := jobs.GetByID(context.Background(), view.ID)
	if job.Status != domain.JobFailed || job.TerminalReason != domain.TerminalProviderRejectedInput {
		t.Fatalf("%+v", job)
	}
	d, _, _ := designs.GetByID(context.Background(), designID)
	if d.Status != domain.DesignGenerationFailed {
		t.Fatalf("status=%s", d.Status)
	}
}

func TestGenerateWorker_OverlapProcess_WinnerStaysSucceeded(t *testing.T) {
	t.Parallel()
	svc, worker, designs, jobs, _, _, prov, designID, token, _ := genFixture(t)
	view, err := svc.RequestGenerate(context.Background(), designID, token, "overlap-1", "")
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	release := make(chan struct{})
	orig := prov.inner
	prov.inner = &gatedProvider{inner: orig, gate: gate, release: release}

	errCh := make(chan error, 2)
	go func() { errCh <- worker.Process(context.Background(), view.ID, designID, 1) }()
	<-gate // first delivery entered provider
	errCh <- worker.Process(context.Background(), view.ID, designID, 1) // peer must lose claim
	close(release)
	var errs [2]error
	errs[0] = <-errCh
	errs[1] = <-errCh
	for _, e := range errs {
		if e != nil && !errors.Is(e, platformjobs.ErrSkipOutcome) {
			t.Fatalf("process: %v", e)
		}
	}
	job, _, _ := jobs.GetByID(context.Background(), view.ID)
	if job.Status != domain.JobSucceeded || job.ArtworkAssetID == "" {
		t.Fatalf("winner wiped: %+v", job)
	}
	d, _, _ := designs.GetByID(context.Background(), designID)
	if d.Status != domain.DesignPreviewReady {
		t.Fatalf("design=%s", d.Status)
	}
	if prov.calls != 1 {
		t.Fatalf("provider calls=%d want 1", prov.calls)
	}
}

func TestGenerateWorker_LastAttemptJobLoadUsesDesignHint(t *testing.T) {
	t.Parallel()
	svc, worker, designs, jobsRepo, _, _, _, designID, token, _ := genFixture(t)
	view, err := svc.RequestGenerate(context.Background(), designID, token, "load-fail-1", "")
	if err != nil {
		t.Fatal(err)
	}
	// Fail the first GetByID; follow-up re-read must succeed so we can terminalize.
	broken := &failGetOnceRepo{failGetJobRepo: &failGetJobRepo{inner: jobsRepo, failID: view.ID}}
	worker.Jobs = broken
	if err := worker.Process(context.Background(), view.ID, designID, 3); err != nil {
		t.Fatalf("last attempt: %v", err)
	}
	job, _, _ := jobsRepo.GetByID(context.Background(), view.ID)
	if job.Status != domain.JobFailed {
		t.Fatalf("%+v", job)
	}
	d, _, _ := designs.GetByID(context.Background(), designID)
	if d.Status != domain.DesignGenerationFailed {
		t.Fatalf("status=%s", d.Status)
	}
}

func TestGenerateWorker_LastAttemptLoadStillBrokenDoesNotAck(t *testing.T) {
	t.Parallel()
	svc, worker, _, jobsRepo, _, _, _, designID, token, _ := genFixture(t)
	view, err := svc.RequestGenerate(context.Background(), designID, token, "load-fail-2", "")
	if err != nil {
		t.Fatal(err)
	}
	broken := &failGetJobRepo{inner: jobsRepo, failID: view.ID}
	worker.Jobs = broken
	err = worker.Process(context.Background(), view.ID, designID, 3)
	if err == nil {
		t.Fatal("want error so platform Complete is not called")
	}
	job, _, _ := jobsRepo.GetByID(context.Background(), view.ID)
	if job.Status != domain.JobQueued && job.Status != domain.JobRunning {
		t.Fatalf("job must stay active when load stays broken, got %+v", job)
	}
}

func TestGenerateWorker_LastAttemptLoadWithArtworkFinishes(t *testing.T) {
	t.Parallel()
	svc, worker, designs, jobs, assets, _, _, designID, token, _ := genFixture(t)
	view, err := svc.RequestGenerate(context.Background(), designID, token, "load-art-1", "")
	if err != nil {
		t.Fatal(err)
	}
	job, _, _ := jobs.GetByID(context.Background(), view.ID)
	_ = job.ClaimForWork(time.Now().UTC(), 0)
	job.ArtworkAssetID = "art-already"
	jobs.mu.Lock()
	jobs.byID[job.ID] = job
	jobs.mu.Unlock()
	_ = assets.Insert(context.Background(), domain.Asset{
		ID: "art-already", Type: domain.AssetTypeArtwork, StorageKey: "private/blobs/metaldogs/artwork/x.png",
		MimeType: "image/png", SizeBytes: 1, Width: 1, Height: 1, Checksum: "sha256:ab",
		OwnerRef: designID, DeleteAfter: time.Now().UTC().Add(time.Hour),
	})

	broken := &failGetOnceRepo{failGetJobRepo: &failGetJobRepo{inner: jobs, failID: view.ID}}
	worker.Jobs = broken
	if err := worker.Process(context.Background(), view.ID, designID, 3); err != nil {
		t.Fatalf("last attempt with artwork: %v", err)
	}
	job, _, _ = jobs.GetByID(context.Background(), view.ID)
	if job.Status != domain.JobSucceeded || job.ArtworkAssetID != "art-already" {
		t.Fatalf("want succeeded with artwork kept, got %+v", job)
	}
	d, _, _ := designs.GetByID(context.Background(), designID)
	if d.Status != domain.DesignPreviewReady {
		t.Fatalf("design=%s", d.Status)
	}
}

type failGetOnceRepo struct {
	*failGetJobRepo
	gets int
}

func (f *failGetOnceRepo) GetByID(ctx context.Context, id string) (domain.GenerationJob, bool, error) {
	f.gets++
	if f.gets == 1 && id == f.failID {
		return domain.GenerationJob{}, false, errors.New("db down")
	}
	return f.inner.GetByID(ctx, id)
}

func TestGenerateService_IdempotencyTextsMismatch(t *testing.T) {
	t.Parallel()
	svc, _, designs, _, _, _, _, designID, token, _ := genFixture(t)
	view, err := svc.RequestGenerate(context.Background(), designID, token, "text-1", "")
	if err != nil {
		t.Fatal(err)
	}
	designs.mu.Lock()
	d := designs.byID[designID]
	d.Texts = map[string]string{"dog_name": "Azor"}
	// Keep generating so RequestGenerate hits idempotency before transition checks…
	// Design is generating — RequestGeneration would 409. Reset to draft-like for text edit path:
	d.Status = domain.DesignDraft
	d.GenerationAttempts = 0
	d.LatestJobID = ""
	designs.byID[designID] = d
	designs.mu.Unlock()
	// Clear active job index so a new generate can be attempted; idempotency row still exists.
	svc.Jobs.(*genJobRepo).mu.Lock()
	delete(svc.Jobs.(*genJobRepo).active, designID)
	// Keep byIdem so replay finds prior job with old texts digest.
	svc.Jobs.(*genJobRepo).mu.Unlock()
	_, err = svc.RequestGenerate(context.Background(), designID, token, "text-1", "")
	var ge *application.GenerateError
	if err == nil || !errors.As(err, &ge) || ge.Code != application.GenerateErrConflict || ge.JobID != view.ID {
		t.Fatalf("want texts conflict, got %v", err)
	}
}

type gatedProvider struct {
	inner   generation.ImageProvider
	gate    chan struct{}
	release chan struct{}
}

func (g *gatedProvider) Name() string { return g.inner.Name() }
func (g *gatedProvider) Generate(ctx context.Context, req generation.GenerationRequest) (generation.GenerationResult, error) {
	select {
	case g.gate <- struct{}{}:
	default:
	}
	<-g.release
	return g.inner.Generate(ctx, req)
}

type failGetJobRepo struct {
	inner  *genJobRepo
	failID string
}

func (f *failGetJobRepo) Insert(ctx context.Context, j domain.GenerationJob) error {
	return f.inner.Insert(ctx, j)
}
func (f *failGetJobRepo) GetByID(ctx context.Context, id string) (domain.GenerationJob, bool, error) {
	if id == f.failID {
		return domain.GenerationJob{}, false, errors.New("db down")
	}
	return f.inner.GetByID(ctx, id)
}
func (f *failGetJobRepo) GetByIdempotency(ctx context.Context, designID, key string) (domain.GenerationJob, bool, error) {
	return f.inner.GetByIdempotency(ctx, designID, key)
}
func (f *failGetJobRepo) GetActiveByDesign(ctx context.Context, designID string) (domain.GenerationJob, bool, error) {
	return f.inner.GetActiveByDesign(ctx, designID)
}
func (f *failGetJobRepo) BeginGenerating(ctx context.Context, j domain.GenerationJob, d domain.Design, sourceDeleteAfter time.Time) error {
	return f.inner.BeginGenerating(ctx, j, d, sourceDeleteAfter)
}
func (f *failGetJobRepo) ClaimForWork(ctx context.Context, id string, lease time.Duration) (domain.GenerationJob, error) {
	return f.inner.ClaimForWork(ctx, id, lease)
}
func (f *failGetJobRepo) ClaimLeaseExpired(ctx context.Context, id string, lease time.Duration) (bool, error) {
	return f.inner.ClaimLeaseExpired(ctx, id, lease)
}
func (f *failGetJobRepo) InvalidateClaim(ctx context.Context, id string) error {
	return f.inner.InvalidateClaim(ctx, id)
}
func (f *failGetJobRepo) Update(ctx context.Context, j domain.GenerationJob, expectedStatus string) error {
	return f.inner.Update(ctx, j, expectedStatus)
}
func (f *failGetJobRepo) CompleteSuccess(ctx context.Context, j domain.GenerationJob, artwork domain.Asset, design domain.Design) error {
	return f.inner.CompleteSuccess(ctx, j, artwork, design)
}
func (f *failGetJobRepo) FinalizeSuccess(ctx context.Context, j domain.GenerationJob, design domain.Design) error {
	return f.inner.FinalizeSuccess(ctx, j, design)
}
func (f *failGetJobRepo) CompleteFailure(ctx context.Context, j domain.GenerationJob, design domain.Design) error {
	return f.inner.CompleteFailure(ctx, j, design)
}
func (f *failGetJobRepo) FailActive(ctx context.Context, id, designID, terminalReason, errorCode string, claimToken int64, at time.Time) error {
	return f.inner.FailActive(ctx, id, designID, terminalReason, errorCode, claimToken, at)
}

var _ ports.GenerationJobRepository = (*failGetJobRepo)(nil)

func TestGenerateService_MissingIdempotencyKey(t *testing.T) {
	t.Parallel()
	svc, _, _, _, _, _, _, designID, token, _ := genFixture(t)
	_, err := svc.RequestGenerate(context.Background(), designID, token, "", "")
	var ge *application.GenerateError
	if err == nil || !errors.As(err, &ge) || ge.Code != application.GenerateErrBadRequest {
		t.Fatalf("err=%v", err)
	}
}

func genFixture(t *testing.T) (
	*application.GenerateService,
	*application.GenerateWorker,
	*designMemRepo,
	*genJobRepo,
	*designAssetRepo,
	*designBlobStore,
	*countingProvider,
	string, string, string,
) {
	t.Helper()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	ids := testutil.NewSeqGenerator("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa")
	clock := testutil.NewFakeClock(now)
	designID := ids.New()
	uploadID := ids.New()
	tplID := ids.New()
	token, hash, err := application.MintOwnerToken()
	if err != nil {
		t.Fatal(err)
	}
	key := "private/blobs/metaldogs/source/2026/11/" + uploadID + ".jpg"
	photo := []byte("photo-bytes-not-empty-xxxxxxxx")
	designs := &designMemRepo{byID: map[string]domain.Design{
		designID: {
			ID: designID, TemplateID: tplID, TemplateVersion: 1, Status: domain.DesignDraft,
			Texts: map[string]string{"dog_name": "Burek"}, SourceAssetID: uploadID,
			OwnerTokenHash: hash, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
		},
	}}
	assets := &designAssetRepo{byID: map[string]domain.Asset{
		uploadID: {
			ID: uploadID, Type: domain.AssetTypeSource, StorageKey: key,
			MimeType: "image/jpeg", SizeBytes: int64(len(photo)), Width: 64, Height: 64,
			Checksum: "sha256:ab", DeleteAfter: now.Add(time.Hour), OwnerRef: designID,
		},
	}}
	store := &designBlobStore{objects: map[string][]byte{key: photo}}
	jobs := newGenJobRepo(designs, assets)
	prov := &countingProvider{inner: generation.Fake()}
	tplStore := &designTplStore{rows: map[string]domain.Template{
		tplID: {
			ID: tplID, Slug: "t", Status: domain.TemplatePublished, Version: 1,
			PromptInstructions: domain.PromptInstructions{Text: "metal dog", PromptVersion: "v1"},
			ModelParameters:    domain.ModelParameters{Model: "fake", Width: 64, Height: 64, Seed: 1},
			OutputConfig: domain.OutputConfig{
				CanvasWidth: 64, CanvasHeight: 64, Monochrome: true,
				PrintArea: domain.PrintArea{X: 0, Y: 0, Width: 64, Height: 64},
			},
		},
	}}
	enqueue := func(context.Context, string, string) error { return nil }
	svc := &application.GenerateService{
		Designs: designs, Jobs: jobs, Assets: assets, IDs: ids, Clock: clock,
		Enqueue: enqueue, Provider: "fake", MaxAttempts: 3,
	}
	worker := &application.GenerateWorker{
		Designs: designs, Jobs: jobs, Assets: assets, Templates: tplStore, Store: store,
		Provider: prov, IDs: ids, Clock: clock, MaxQueueAttempts: 3,
	}
	return svc, worker, designs, jobs, assets, store, prov, designID, token, key
}
