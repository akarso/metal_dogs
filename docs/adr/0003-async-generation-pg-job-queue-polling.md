# ADR-0003: Asynchronous generation on Shopanda's PostgreSQL job queue with HTTP polling

- **Status:** Proposed (to be Accepted at close of `F1`, revisited in `C4`)
- **Date:** 2026-10-15
- **Related slices:** `F1`, `F3`, `C4`, `G5`, `E4`, `P2`, `O2`
- **Related ADRs:** [ADR-0001](0001-metal-dogs-as-shopanda-external-plugin.md)

## Context

Image generation takes longer than an HTTP request should. `AGENTS.md` requires: create a job, return its id, expose status; polling by default; in-process events and a database-backed job queue are valid first implementations; no broker or event sourcing by default.

Shopanda (verified) provides a PostgreSQL-backed job queue with retry, admin API, `jobs:list/show/retry/cancel` CLI, a cron scheduler, and runtime modes `serve` / `worker` / `scheduler`. Whether a plugin can **register custom job types and handlers** on that queue is not yet verified.

## Decision

1. All long-running work runs as jobs on **Shopanda's job queue**: `metaldogs.generate`, `metaldogs.prepare_production_artwork`, `metaldogs.snapshot_order_design`, `metaldogs.submit_fulfillment`, and scheduler tasks `metaldogs.retention_sweep`, `metaldogs.reconcile_snapshots`.
2. HTTP endpoints enqueue and return `202` with a job id; clients poll `GET /api/customizer/generation-jobs/{id}` with server-suggested `retry_after`. SSE or WebSockets are not introduced (slice `X5` only if justified by measurements).
3. Job enqueue happens in the **same database transaction** as the state change that caused it (PostgreSQL queue makes this possible), which removes the need for a separate outbox for plugin-owned state. For Shopanda core events (order created, payment confirmed) the handler enqueues an idempotent job and a scheduler reconciliation sweep repairs missed events.
4. Retry policy: transient errors only, bounded (`max_retries`, default 3), exponential backoff with jitter; permanent failures go to a terminal `failed` state with a stable `terminal_reason`.
5. Per-design ordering and single-flight are enforced by a partial unique index on active jobs per design, not by queue features.
6. **Fallback if `F1` finds custom job types unsupported:** a plugin-owned `metaldogs_jobs` table with `SELECT ... FOR UPDATE SKIP LOCKED` leasing, processed by a plugin CLI command `metaldogs:worker` run as an additional process. Same contracts, same statuses; the queue port in the plugin hides the difference.

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

## Verification (to be filled in F1)

- Custom job type registration: _pending_
- Plugin migration mechanism: _pending_
- Lease/visibility timeout semantics: _pending_
