package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

func TestGenerationJobStatuses_Golden(t *testing.T) {
	t.Parallel()
	want := []string{"queued", "running", "validating", "succeeded", "failed", "cancelled"}
	if len(domain.GenerationJobStatuses) != len(want) {
		t.Fatalf("len=%d", len(domain.GenerationJobStatuses))
	}
	for i := range want {
		if domain.GenerationJobStatuses[i] != want[i] {
			t.Fatalf("[%d]=%q", i, domain.GenerationJobStatuses[i])
		}
	}
}

func TestGenerationJob_ClaimValidateSucceed(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	j := domain.NewQueuedJob("j1", "d1", "idem-1", "fake", 1, now, "corr", "src-1", 1, "digest")
	if err := j.ClaimForWork(now, domain.DefaultClaimLease); err != nil || j.Status != domain.JobRunning {
		t.Fatalf("%v %+v", err, j)
	}
	// Fresh running claim is held.
	if err := j.ClaimForWork(now.Add(time.Second), domain.DefaultClaimLease); !errors.Is(err, domain.ErrClaimHeld) {
		t.Fatalf("want claim held, got %v", err)
	}
	// Redelivery from validating is claimable.
	j.Status = domain.JobValidating
	if err := j.ClaimForWork(now, domain.DefaultClaimLease); err != nil || j.Status != domain.JobRunning {
		t.Fatalf("reclaim validating: %v %+v", err, j)
	}
	// Expired running lease is reclaimable.
	old := now.Add(-domain.DefaultClaimLease - time.Second)
	j.StartedAt = &old
	if err := j.ClaimForWork(now, domain.DefaultClaimLease); err != nil || j.StartedAt == nil || !j.StartedAt.Equal(now) || j.ClaimToken < 2 {
		t.Fatalf("reclaim expired running: %v %+v", err, j)
	}
	cost := 0.0
	if err := j.Succeed("art-1", "mock-1", []byte(`{"status":"pass"}`), 12, &cost, "USD", "fake-g0-v1", "v1", []byte(`{}`), now); err != nil {
		t.Fatal(err)
	}
	if j.Status != domain.JobSucceeded || !j.IsTerminal() || j.ArtworkAssetID != "art-1" {
		t.Fatalf("%+v", j)
	}
}

func TestGenerationJob_FailAndRequeue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	j := domain.NewQueuedJob("j1", "d1", "k", "fake", 1, now, "", "s", 1, "")
	_ = j.ClaimForWork(now, 0)
	if j.StartedAt == nil {
		t.Fatal("started_at")
	}
	if err := j.RequeueForRetry(); err != nil || j.Status != domain.JobQueued || j.StartedAt != nil {
		t.Fatalf("%v %+v", err, j)
	}
	_ = j.ClaimForWork(now, 0)
	if err := j.Fail(domain.TerminalProviderError, "permanent", now); err != nil {
		t.Fatal(err)
	}
	var it *domain.ErrInvalidTransition
	if err := j.ClaimForWork(now, 0); !errors.As(err, &it) {
		t.Fatalf("want invalid transition, got %v", err)
	}
	j.ArtworkAssetID = "art-leftover"
	j.MockupAssetID = "mock-leftover"
	j.ForceFail(domain.TerminalCancelled, "design_conflict", now)
	if j.Status != domain.JobFailed || j.ArtworkAssetID != "art-leftover" || j.MockupAssetID != "" {
		t.Fatalf("ForceFail must keep persisted artwork and clear mockup: %+v", j)
	}
}

func TestGenerationJob_SucceedRequiresMockup(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	j := domain.NewQueuedJob("j1", "d1", "k", "fake", 1, now, "", "s", 1, "")
	_ = j.ClaimForWork(now, 0)
	cost := 0.0
	if err := j.Succeed("art-1", "", nil, 1, &cost, "USD", "m", "v", nil, now); err == nil {
		t.Fatal("empty mockup must fail")
	}
}

func TestGenerationJob_InputMatches(t *testing.T) {
	t.Parallel()
	digest := domain.TextsDigestFor(map[string]string{"dog_name": "Burek"})
	j := domain.GenerationJob{SourceAssetID: "a", TemplateVersion: 2, TextsDigest: digest}
	if !j.InputMatches("a", 2, digest) || j.InputMatches("b", 2, digest) || j.InputMatches("a", 1, digest) {
		t.Fatal("input match")
	}
	if j.InputMatches("a", 2, "other") {
		t.Fatal("texts digest must differ")
	}
	legacy := domain.GenerationJob{}
	if legacy.InputMatches("x", 9, digest) {
		t.Fatal("empty scope must not match populated inputs")
	}
}

func TestDesign_RequestGeneration(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	d := domain.Design{Status: domain.DesignDraft, ExpiresAt: now.Add(time.Hour)}
	if err := d.RequestGeneration("job-1", 3, now); err != nil {
		t.Fatal(err)
	}
	if d.Status != domain.DesignGenerating || d.GenerationAttempts != 1 || d.LatestJobID != "job-1" {
		t.Fatalf("%+v", d)
	}
	d.Status = domain.DesignPreviewReady
	d.GenerationAttempts = 3
	var lim *domain.ErrLimitReached
	if err := d.RequestGeneration("job-3", 3, now); !errors.As(err, &lim) {
		t.Fatalf("want limit, got %v", err)
	}
}

func TestGenerationAttemptsRemaining(t *testing.T) {
	t.Parallel()
	if got := domain.GenerationAttemptsRemaining(0, 3); got != 3 {
		t.Fatalf("got %d", got)
	}
	if got := domain.GenerationAttemptsRemaining(3, 3); got != 0 {
		t.Fatalf("got %d", got)
	}
	if got := domain.GenerationAttemptsRemaining(5, 3); got != 0 {
		t.Fatalf("got %d", got)
	}
}

func TestCheckGenerationCooldown(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	if err := domain.CheckGenerationCooldown(time.Time{}, domain.DefaultGenerationCooldown, now); err != nil {
		t.Fatal(err)
	}
	if err := domain.CheckGenerationCooldown(now.Add(-time.Second), 0, now); err != nil {
		t.Fatal("cooldown 0 must disable wait")
	}
	if err := domain.CheckGenerationCooldown(now.Add(-domain.DefaultGenerationCooldown), domain.DefaultGenerationCooldown, now); err != nil {
		t.Fatal(err)
	}
	var cd *domain.ErrCooldown
	if err := domain.CheckGenerationCooldown(now.Add(-5*time.Second), domain.DefaultGenerationCooldown, now); !errors.As(err, &cd) {
		t.Fatalf("want cooldown, got %v", err)
	}
	if cd.RetryAfter < 24*time.Second || cd.RetryAfter > 26*time.Second {
		t.Fatalf("retry_after=%s", cd.RetryAfter)
	}
	if got := domain.CooldownRetryAfterSeconds(1500*time.Millisecond, 30*time.Second); got != 2 {
		t.Fatalf("ceil Retry-After=%d", got)
	}
	if got := domain.CooldownRetryAfterSeconds(40*time.Second, 30*time.Second); got != 30 {
		t.Fatalf("clamp Retry-After=%d", got)
	}
}

func TestGenerationJobCooldownAnchor(t *testing.T) {
	t.Parallel()
	queued := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	finished := queued.Add(2 * time.Minute)
	j := domain.GenerationJob{Status: domain.JobRunning, QueuedAt: queued, FinishedAt: &finished}
	if _, ok := domain.GenerationJobCooldownAnchor(j); ok {
		t.Fatal("running must not anchor")
	}
	if !j.IsActive() {
		t.Fatal("running must be active")
	}
	j.Status = domain.JobSucceeded
	got, ok := domain.GenerationJobCooldownAnchor(j)
	if !ok || !got.Equal(finished) {
		t.Fatalf("succeeded anchor=%v ok=%v", got, ok)
	}
	j.FinishedAt = nil
	if _, ok := domain.GenerationJobCooldownAnchor(j); ok {
		t.Fatal("terminal without finished_at must not anchor")
	}
}

func TestDesign_MarkPreviewReadyAndFailed(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	d := domain.Design{Status: domain.DesignGenerating}
	if err := d.MarkPreviewReady(now); err != nil || d.Status != domain.DesignPreviewReady {
		t.Fatalf("%v %+v", err, d)
	}
	d.Status = domain.DesignGenerating
	if err := d.MarkGenerationFailed(now); err != nil || d.Status != domain.DesignGenerationFailed {
		t.Fatalf("%v %+v", err, d)
	}
}
