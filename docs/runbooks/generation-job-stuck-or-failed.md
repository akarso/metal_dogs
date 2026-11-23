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
4. **Correlation / duration (platform + F3):** Search worker logs by `request_id` (inbound `X-Request-ID`; Metal Dogs also logs `correlation_id`). Job lines include `attempt` and `duration_ms`. Metrics: `shopanda_job_results_total` / `shopanda_job_duration_seconds` (after durable Complete/Fail attempts). Coarse readiness: `GET /api/customizer/health` (`Host.Jobs()` bound / `CapJobs` only; 503 when degraded). Queue depths: admin `GET /api/v1/admin/customizer/health` (`jobs.read`) via `Host.Jobs().CountsByType`, or `jobs:list`. Platform contract: [shopanda#568](https://github.com/akarso/shopanda/pull/568).
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
3. **Stuck platform `processing`** (same id as product `generation_job_id`, `updated_at` older than the reclaim age) → `./app jobs:requeue <job_id>` (default `--min-age=5m`). That keeps `attempts` and runs `OnRequeued` in the same DB transaction as the re-pend, which bumps the product `claim_token` before the row is visible as `pending`. The fence is `Host.OnRequeued` registered in plugin `Init`. A hook error leaves the platform row `processing`. `./app jobs:retry` does not reclaim `processing`: it accepts only `failed`, sets `attempts` to 0, and does not run `OnRequeued` (claim token stays). Same-Idempotency-Key customer retry uses enqueue Conflict → `Requeue` for stale `processing`, and `Requeue` with `WithRequeueTerminal` for `failed`/`done`/`cancelled` (attempts kept, `last_error` unchanged, claim bumped in that transaction). A hook error leaves the platform row in its prior status. Live young `processing` is left alone. `queue.driver` other than `postgres` fails plugin Init (`pluginsdk.RequireQueueDriver`; empty driver counts as postgres).
4. **Stuck non-terminal product job** (design `generating`, job `queued`/`running`/`validating`, platform row `failed`/`done`/`cancelled`/missing) → prefer same-key customer retry. Stale platform `processing` is `./app jobs:requeue <id>`. A terminal platform row (`failed`, `done`, or `cancelled`, attempts kept) is `./app jobs:requeue <id> --terminal [--force]`. Bare `--terminal` is the rare non-exhausted `failed` path. `--terminal --force` covers exhausted `failed` and `done`/`cancelled`. `./app jobs:retry <id>` is `failed` only: it sets `attempts` to 0 and does not run `OnRequeued` (claim token stays). If the product row stays wedged, prefer `TBD` `metaldogs:design-reconcile <design_id>`. Until that command exists: operator SQL may set the job to `failed` with `terminal_reason=orphaned` and the design to `generation_failed` in one transaction, then ask the customer to retry.
5. Provider incident → follow the provider outage runbook (cost cap / disable generation).
6. Production artwork failed or still `pending` → use the reason codes below. Fulfil manually only with an operator-approved file ([`manual-pod-fulfillment.md`](manual-pod-fulfillment.md)).

## Production artwork failed

Job type: `integration.sync.metaldogs.prepare_production_artwork` (platform job id = design version id).

```text
./app jobs:list --type=integration.sync.metaldogs.prepare_production_artwork --status=failed --limit=50
./app jobs:show <design_version_id>
```

The stable reason is `metaldogs_design_versions.production_failure_reason` when `production_status = failed`. Logs use `design_version_id`, `design_id`, and `reason`. They do not contain storage keys or image bytes. Do not edit `metaldogs_design_versions` by hand.

`plugins.metaldogs.production_spec` has no defaults. Omit the section and the job stays on `pending_spec`. A present section with a missing or invalid field stops startup. Example keys are in `configs/config.example.product.yaml` and are not a printer size. Real numbers are P0 ([ADR-0009](../adr/0009-pod-provider-and-catalog-modelling.md)).

| Reason | What it means | What to do |
| --- | --- | --- |
| `pending_spec` | The spec section is absent. The version stays `production_status=pending`. The job error contains `pending_spec`. This delivery is the last (`max_retries` set to `attempts`). | Configure a complete `production_spec` (every field). The version is still `pending`, so reclaim the failed job with the existing prepare rules (`failed` → pending, `attempts=0`). Do not invent pixel sizes. |
| `too_small` | The artwork is smaller than the spec and `upscale_policy` is `reject`. The version is `failed`. | Reclaiming the job repeats `too_small` and does not refit. Do not crop. A new approval is a new version: either a larger artwork, or `upscale_policy: upscale` chosen on purpose before that approval. |
| `aspect_mismatch` | Artwork aspect differs from the spec by more than 1%. The version is `failed`. | Do not crop or pad. Reclaiming the job does not refit. A new approval is required, with artwork or a spec whose aspects match. |
| `transparency_required` | The spec requires an alpha channel and the PNG has none. The version is `failed`. | Do not send the file to a transparency print. Reclaiming the job does not refit. New artwork with alpha and a new approval, or a spec that does not require transparency when the printer does not need it, then a new approval. |
| `artwork_unreadable` | The stored artwork is missing or not a decodable PNG. The version is `failed`. | Reclaiming the job does not reread a new file onto this version. Regenerate and approve again. |

A version already `ready` makes a later run a no-op (no new write). Storage or database errors retry (`MaxRetries` 3) and leave the version `pending` or `preparing`. After `ready`, snapshot fill errors are logged (`metaldogs.prepare_production_artwork.fill_snapshot`) and the job still succeeds; `reconcile_snapshots` fills NULL production columns later when `order_snapshot.enabled` is on and an order source is bound.

Irreversible: none of the above deletes data. Never edit `metaldogs_design_versions` or snapshots by hand.

## Verification

- Queue depth returns below threshold; new jobs reach `succeeded` within p95; affected designs show `preview_ready` or a clear failure with retry available. After `jobs:requeue` on a stale `processing` row, `jobs:show` reports `pending` then a new worker delivery. `jobs:retry` on that row errors because the status is not `failed`.

## Prevention / follow-up

- Every new `terminal_reason` must be added to this runbook and to `docs/architecture/flows/generation-pipeline.md`.
- Add a regression test for the failure class observed.
- Generate enqueue recovery (`Requeue` on Conflict) requires `queue.driver=postgres`.
