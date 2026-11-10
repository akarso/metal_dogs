# Runbook: generation job stuck or failed

- **Introduced by slice:** `C4` (generation), `G5` (production artwork)
- **Last exercised:** C4 unit/feature (fake provider); production never
- **Severity when triggered:** high if many designs affected; medium for a single design
- **Owner:** on-call operator

## Symptom

- Alert: `integration.sync.metaldogs.generate` queue depth above threshold, or job failure rate above threshold, or p95 job duration above threshold.
- Customer report: progress screen never completes, or shows "temporarily unavailable".
- Admin: `DesignVersion.production_status = failed` count rising (production artwork variant).

## Impact

Customers cannot reach preview or approval; each failed attempt may have cost money; production artwork failures block automated fulfilment (manual path remains available).

## Diagnosis

1. Is the worker running? Check the `app worker` process/health. If no worker leases jobs, everything sits in `pending`.
2. List recent jobs (upstream CLI, verify against pinned tag):
   `./app jobs:list --type=integration.sync.metaldogs.generate --status=failed --limit=50`
   `./app jobs:list --type=integration.sync.metaldogs.generate --status=processing --limit=50`
3. Inspect platform queue row: `./app jobs:show <job_id>`. Product job state lives in `metaldogs_generation_jobs` (`status`, `attempt`, `terminal_reason`, `error_code`, `provider`, `latency_ms`, `artwork_asset_id`). Correlate via payload `generation_job_id` (platform job id equals product generation job id).
4. **Correlation / duration (platform + F3):** Search worker logs by `request_id` (inbound `X-Request-ID`; Metal Dogs also logs `correlation_id`). Job lines include `attempt` and `duration_ms`. Metrics: `shopanda_job_results_total` / `shopanda_job_duration_seconds` (after durable Complete/Fail attempts). Coarse readiness: `GET /api/customizer/health` (BoundQueue/Reader only; 503 when degraded). Queue depths: admin `GET /api/v1/admin/customizer/health` (`jobs.read`) or `jobs:list` / `CountsByType`. Platform contract: [shopanda#568](https://github.com/akarso/shopanda/pull/568).
5. Classify by `terminal_reason`:
   - `provider_timeout` / `provider_error` / `provider_rate_limited` across many jobs → provider incident; go to [`ai-provider-outage-and-cost-cap.md`](ai-provider-outage-and-cost-cap.md).
   - `cost_cap_reached` → same runbook, cost section.
   - `validation_failed` clustered on one `template_slug` → template or prompt regression; go to [`template-publishing.md`](template-publishing.md).
   - `storage_error` → object storage credentials/availability.
   - `attempts_exhausted` → product job is terminal; follow remediation 2 (do not expect self-heal).
6. Jobs in `processing` longer than the stage lease (~2 minutes): worker crash or hung `Read`/`Put`/provider call. Check worker logs by `job_id` / `request_id`. A `processing` row does **not** resume on worker restart — only `pending` rows are dequeued.
7. Design-level check (SQL, ids only): design `status = generating` with no active job → inconsistency; see remediation 4.

## Remediation

1. Worker down → restart `app worker`; **pending** jobs resume automatically. A platform row stuck in `processing` does **not** resume — use remediation 3.
2. **Terminal product job** (`metaldogs_generation_jobs.status IN ('succeeded','failed','cancelled')`) → do **not** `jobs:retry` the platform row. The handler no-ops on terminal product jobs and will not call the provider. Let the customer regenerate (new Idempotency-Key) within their attempt limit.
3. **Stuck platform `processing`** (same id as product `generation_job_id`, `updated_at` older than ~2 minutes) → `./app jobs:retry <job_id>` (or admin retry) reclaims stale processing back to `pending` (attempts preserved) and invalidates the product `claim_token` in the same DB transaction before the row is visible as `pending`. The generate fence is installed at queue bind / `newJobsService` when Metal Dogs is enabled — a missing hook aborts the re-pend. If claim fencing fails, retry reports an error and the platform row stays `processing`. Same-Idempotency-Key customer retry also calls enqueue Conflict → `Requeue` for failed/done/cancelled/stale-processing when `queue.driver=postgres` (live young `processing` is left alone). Requires Postgres job queue; broker drivers do not expose this recovery path.
4. **Stuck non-terminal product job** (design `generating`, job `queued`/`running`/`validating`, platform row `failed`/`done`/missing) → prefer same-key customer retry (re-pends via Conflict/`Requeue`) or `./app jobs:retry <id>` when failed/stale-processing. If the product row stays wedged, prefer `TBD` `metaldogs:design-reconcile <design_id>`. Until that command exists: operator SQL may set the job to `failed` with `terminal_reason=orphaned` and the design to `generation_failed` in one transaction, then ask the customer to retry.
5. Provider incident → follow the provider outage runbook (cost cap / disable generation).
6. Production artwork `failed` → read the spec validation code; if the artwork cannot meet the spec, contact the customer via the order; fulfil manually only with an operator-approved file ([`manual-pod-fulfillment.md`](manual-pod-fulfillment.md)).

Irreversible: none of the above deletes data. Never edit `metaldogs_design_versions` or snapshots by hand.

## Verification

- Queue depth returns below threshold; new jobs reach `succeeded` within p95; affected designs show `preview_ready` or a clear failure with retry available. After `jobs:retry` on a stale `processing` row, `jobs:show` reports `pending` then a new worker delivery.

## Prevention / follow-up

- Every new `terminal_reason` must be added to this runbook and to `docs/architecture/flows/generation-pipeline.md`.
- Add a regression test for the failure class observed.
- Generate enqueue recovery (`Requeue` on Conflict) requires `queue.driver=postgres`.
