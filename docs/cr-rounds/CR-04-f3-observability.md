# CR-04: F3 observability baseline

- Closed: 2026-11-06 (vendor facade); #568 merge on pin 2026-11-08 (`a8cbcb18`); F3 residuals still product-only
- Slices: `F3`
- Milestone progress: M0 → 4/6 slices (`F0`–`F3`)

## What shipped

**Shopanda (platform, [PR #568](https://github.com/akarso/shopanda/pull/568)):** `request_id` injection on sync Enqueue, `SyncJobContext.Attempts`, `metrics.JobResult` (+ Prometheus, clamped outcomes), `jobs.Reader.CountsByType`, worker logs/`JobResult` after durable Complete, guide `SYNC_JOB_OBSERVABILITY.md` (upstream). Merge present at pin [`a8cbcb18`](../UPSTREAM_PIN.md).

**Still Metal Dogs–only platform deltas** (health/correlation depend on these; see overview deviations): `App.BoundQueue()`, sync-handler payload `request_id`→ctx reinjection.

**Metal Dogs (vendor minimum):**
- `observability.CorrelationID` / `JobLogFields` (`correlation_id` = platform `request_id`); sync_runtime reinjects payload `request_id` into handler ctx
- Public `GET /api/customizer/health` — BoundQueue/Reader probe only (no `CountsByType`); coarse `ok|degraded` + generic `reason`, HTTP 503 when degraded (not a k8s probe)
- Admin `GET /api/v1/admin/customizer/health` (`jobs.read`) — per-type depths via `BoundQueue` + `CountsByType`; types retained with `error` on query failure
- `RegisterManual("obs.ping")` — Enqueue `request_id` injection + handler ctx reinjection + handler log fields (`correlation_id`). Full HTTP middleware → worker log E2E remains a follow-up.

## Threat model (public health)

Unauthenticated depth counts would expose backlog / failure pressure. Depths and typed degrade reasons stay behind admin auth; public surface is a cheap BoundQueue/Reader readiness check only (no per-type SQL).

## Verification results

| Item | Result |
| --- | --- |
| Enqueue injects `request_id` | `TestObsPing_EnqueuePropagatesRequestID` |
| Handler logs `correlation_id` + `attempt` | same |
| Payload → ctx reinjection | `TestRegisterSyncJobHandlers_InjectsPayloadRequestIDIntoCtx` |
| Admin depths / BoundQueue / counts error | `httpapi` health tests |
| JobResult after Complete | `TestWorker_CompleteFailure_RecordsMetric` |
| ImportBoundary | Pass |

## Flags / migrations / rollback

- No new flags. Plugin still default off.
- Rollback: disable plugin / revert CR; platform metrics are additive.

## Documentation updated

- [x] ROADMAP F3 DONE for vendor + #568 merge; residuals (`BoundQueue`, sync reinjection) remain on overview allowlist
- [x] This CR + log link

## Follow-ups

- F2 billing gate / require `CI / integration-metaldogs` still open
- Batch `CountsByType` / `(type, status)` index before many `integration.sync.metaldogs.*` types
- Full HTTP `X-Request-ID` → middleware → Enqueue → worker → log E2E
