# Runbook: generation job stuck or failed

- **Introduced by slice:** `C4` (generation), `G5` (production artwork)
- **Last exercised:** never
- **Severity when triggered:** high if many designs affected; medium for a single design
- **Owner:** on-call operator

## Symptom

- Alert: `integration.sync.metaldogs.generate` queue depth above threshold, or job failure rate above threshold, or p95 job duration above threshold.
- Customer report: progress screen never completes, or shows "temporarily unavailable".
- Admin: `DesignVersion.production_status = failed` count rising (production artwork variant).

## Impact

Customers cannot reach preview or approval; each failed attempt may have cost money; production artwork failures block automated fulfilment (manual path remains available).

## Diagnosis

1. Is the worker running? Check the `app worker` process/health. If no worker leases jobs, everything sits in `queued`.
2. List recent jobs (upstream CLI, verify against pinned tag):
   `./app jobs:list --type=integration.sync.metaldogs.generate --status=failed --limit=50`
   `./app jobs:list --type=integration.sync.metaldogs.generate --status=processing --limit=50`
3. Inspect one: `./app jobs:show <job_id>`. Read `attempt`, `terminal_reason`, `error_code`, `provider`, `latency_ms`.
4. Classify by `terminal_reason`:
   - `provider_timeout` / `provider_error` / `provider_rate_limited` across many jobs → provider incident; go to [`ai-provider-outage-and-cost-cap.md`](ai-provider-outage-and-cost-cap.md).
   - `cost_cap_reached` → same runbook, cost section.
   - `validation_failed` clustered on one `template_slug` → template or prompt regression; go to [`template-publishing.md`](template-publishing.md).
   - `storage_error` → object storage credentials/availability.
   - `provider_result_lost` → money may have been spent without a stored result; see remediation 4.
   - `attempts_exhausted` with mixed reasons → transient instability; usually self-heals.
5. Jobs in `running` longer than the lease/visibility timeout: worker crash or hang. Check worker logs by `job_id`.
6. Design-level check (SQL, ids only): design `status = generating` with no active job → inconsistency; see remediation 5.

## Remediation

1. Worker down → restart `app worker`; queued jobs resume automatically.
2. Transient provider failure, terminal `failed` → do **not** blindly `jobs:retry`: a retry calls the paid provider again. Retry only if `cost` on the job is zero/null or the operator accepts the cost. Otherwise let the customer regenerate within their limit.
3. Provider incident → follow the provider outage runbook (possibly set the cost cap or disable generation via flag to stop the bleeding).
4. `provider_result_lost` → check the provider dashboard by the stored provider request id; if the image can be fetched, `TBD (C4 follow-up)` command to attach it; otherwise compensate by resetting the design's attempt count for that attempt (`TBD (C7)` command) so the customer is not charged an attempt for our fault.
5. Design `generating` without active job → `TBD (C4)` command `metaldogs:design-reconcile <design_id>` moves it to `generation_failed` with reason `orphaned`; the customer can retry.
6. Production artwork `failed` → read the spec validation code; if the artwork cannot meet the spec, contact the customer via the order; fulfil manually only with an operator-approved file ([`manual-pod-fulfillment.md`](manual-pod-fulfillment.md)).

Irreversible: none of the above deletes data. Never edit `metaldogs_design_versions` or snapshots by hand.

## Verification

- Queue depth returns below threshold; new jobs reach `succeeded` within p95; affected designs show `preview_ready` or a clear failure with retry available.

## Prevention / follow-up

- Every new `terminal_reason` must be added to this runbook and to `docs/architecture/flows/generation-pipeline.md`.
- Add a regression test for the failure class observed.
