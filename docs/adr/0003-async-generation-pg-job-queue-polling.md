# ADR-0003: Asynchronous generation on Shopanda's PostgreSQL job queue with HTTP polling

- **Status:** Accepted (C4 / CR-14)
- **Date:** 2026-10-15
- **Related slices:** `F1`, `F3`, `C4`, `G5`, `E4`, `P2`, `O2`
- **Related ADRs:** [ADR-0001](0001-metal-dogs-as-shopanda-external-plugin.md)

## Context

Image generation takes longer than an HTTP request should. `AGENTS.md` requires: create a job, return its id, expose status; polling by default; in-process events and a database-backed job queue are valid first implementations; no broker or event sourcing by default.

Shopanda (inspected at the F0 pin) provides a PostgreSQL-backed job queue with retry, admin API, `jobs:list/show/retry/cancel` CLI, a cron scheduler, and runtime modes `serve` / `worker` / `scheduler`. Phase 13 added plugin `RegisterManual` / `Enqueue` for on-demand work. Free-form job type strings (e.g. `metaldogs.generate`) are **out of scope** upstream; plugins use `integration.sync.{slug}.{name}`.

## Decision

1. All long-running work runs as Shopanda **integration sync jobs** registered by the `metaldogs` plugin via `pluginsdk` (`RegisterManual` / `RegisterOnEvent` / `RegisterCron` as appropriate). Queue type strings are exactly:
   - `integration.sync.metaldogs.generate`
   - `integration.sync.metaldogs.prepare_production_artwork`
   - `integration.sync.metaldogs.snapshot_order_design`
   - `integration.sync.metaldogs.submit_fulfillment`
   - `integration.sync.metaldogs.retention_sweep`
   - `integration.sync.metaldogs.reconcile_snapshots`
2. HTTP endpoints enqueue (`Enqueue` after composition-root `BindQueue`) and return `202` with a job id; clients poll `GET /api/customizer/generation-jobs/{id}` with server-suggested `retry_after`. SSE or WebSockets are not introduced (slice `X5` only if justified by measurements).
3. Job enqueue happens in the **same database transaction** as the state change that caused it when the queue driver allows (PostgreSQL queue); otherwise enqueue immediately after commit with an idempotent handler and a reconciliation sweep. For Shopanda core events (`order.paid`, and `order.created` with possible later `order.failed`) the handler enqueues an idempotent job and a scheduler reconciliation sweep repairs missed events.
4. Retry policy: transient errors only, bounded (`max_retries`, default 3), exponential backoff with jitter; permanent failures go to a terminal `failed` state with a stable `terminal_reason`.
5. Per-design ordering and single-flight are enforced by a partial unique index on active jobs per design, not by queue features.
6. Do **not** invent free-form `metaldogs.*` queue types. A plugin-owned lease table is only an emergency fallback if upstream sync jobs regress; it is not the planned path after Phase 13.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Synchronous generation in the request | Timeouts, no retry, blocks workers, bad mobile experience |
| Redis/RabbitMQ queue | Shopanda supports them as core plugins, but PostgreSQL is sufficient at this scale and avoids a service |
| SSE from the start | Adds connection management for a 10–60 s wait that polling handles well |
| Goroutine + in-memory state | Lost on restart; cannot be observed or retried |

## Consequences

- Positive: one database, observable via Shopanda's job admin, transactional enqueue, horizontal workers via `app worker`.
- Negative: polling load. Mitigation: backoff with jitter; status endpoint is cheap (single row); caching headers.
- Negative: dependency on Shopanda queue semantics (lease, visibility timeout). Mitigation: contract tests for the behaviours we rely on; fallback path defined.

## Compliance

- Feature test: `POST /generate` returns within the HTTP timeout with the fake provider configured to sleep.
- Feature test: worker crash simulation → job re-leased and completed exactly once (no duplicate asset, no duplicate provider call when cost already recorded).
- `F1` verification result recorded here under "Verification" before status moves to Accepted.

## Verification (F1, 2026-11-06)

Inspected and wired at the F0 pin; confirmed by compiling `plugins/metaldogs` against Shopanda Phase 13 APIs:

| Item | Result |
| --- | --- |
| Custom / free-form job types (`metaldogs.generate`) | **Unsupported** upstream (deferred). Use `pluginsdk` `RegisterManual` / `RegisterOnEvent` / `RegisterCron` → queue types `integration.sync.metaldogs.<name>`. |
| Plugin migration mechanism | **Supported.** `plugin.Migrator` + `Migration()` embed FS; versions `plugin:metaldogs:<basename>`; applied by `app migrate` / boot `InitAll`. F1 ships `001_create_meta.sql` as the first namespaced migration. |
| On-demand enqueue | **Supported.** `Integration.Enqueue` after composition-root `BindQueue` (PR-1087–1088). Generation job registration lands in `C4`. |
| Lease/visibility timeout semantics | **Inspected at pin** (Phase 11 queue); contract tests still owed in `C4`. |

## Verification (C4, 2026-11-10)

| Item | Result |
| --- | --- |
| `RegisterManual("generate")` → `integration.sync.metaldogs.generate` | **Shipped.** Wired in `plugins/metaldogs/sync_generate.go` with `MaxRetries` from config. |
| `POST …/generate` + `Idempotency-Key` → `202` + job id; poll `GET …/generation-jobs/{id}` | **Shipped.** Feature HTTP tests cover accept, replay, auth 404, missing key 400. |
| Enqueue vs design txn | Shopanda `JobQueue.Enqueue` opens its own tx → **post-commit Enqueue** after `BeginGenerating`; handler idempotent. |
| Per-design mutex + idempotency | Partial unique indexes on `metaldogs_generation_jobs`; DSN tests for conflict. |
| Fake provider → `preview_ready` | Application worker tests with Fake + G3 validate + artwork asset (null `owner_ref`). |
| Crash / redelivery | Re-entry with `artwork_asset_id` set skips provider; otherwise reclaim from `queued`/`validating` (or expired `running` lease via DB `NOW()`) and may re-call the provider. Each claim bumps `claim_token`; stale writers no-op / `ErrSkipOutcome`. Platform enqueue uses product job id as PK; Conflict → `Requeue`. Product `claim_token` is invalidated inside the re-pend transaction (`OnRequeued`, installed at BindQueue / `jobs:retry` service construction) before `pending` is visible to `Dequeue`. Missing hook or fence failure aborts the re-pend. Worker re-checks `claim_token` before `Generate` and before durable writes. `Complete`/`Fail` match dequeue `attempts`. |
| Lease/visibility | Relies on Shopanda queue Fail/retry; product tests simulate redelivery, not raw lease SQL. |

Status: **Accepted**.
