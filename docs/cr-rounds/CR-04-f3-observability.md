# CR-04: F3 observability baseline

- Closed: 2026-11-06 (vendor facade); pin absorb still open
- Slices: `F3`
- Milestone progress: M0 → 4/6 slices (`F0`–`F3` vendor); platform absorb gated on [shopanda#568](https://github.com/akarso/shopanda/pull/568) + pin bump

## What shipped

**Shopanda (platform, [PR #568](https://github.com/akarso/shopanda/pull/568)):** `request_id` injection on sync Enqueue, `SyncJobContext.Attempts`, `metrics.JobResult` (+ Prometheus, clamped outcomes), `jobs.Reader.CountsByType`, worker logs/`JobResult` after durable Complete, guide `SYNC_JOB_OBSERVABILITY.md` (upstream). Forward-ported into Metal Dogs until the next upstream pin absorbs the merge (listed under overview deviations). Current pin remains `56818946…` — next bump will conflict with in-tree platform edits until absorb.

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

- [x] ROADMAP F3 vendor DONE / pin absorb blocked; runbook; plugin README; overview deviations
- [x] This CR + log link

## Follow-ups

- Absorb platform changes via Shopanda pin bump when #568 merges to the consumed ref; drop forward-port from deviations; retarget guide links to pin SHA
- F2 billing gate / require `CI / integration-metaldogs` still open
- Batch `CountsByType` / `(type, status)` index before many `integration.sync.metaldogs.*` types
- Full HTTP `X-Request-ID` → middleware → Enqueue → worker → log E2E
- `F4` private object storage
