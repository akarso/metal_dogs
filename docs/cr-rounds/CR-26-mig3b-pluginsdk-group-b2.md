# CR-26: MIG3b pluginsdk group B2 (ticket 01)

- Status: ticket 01 recorded 2026-11-20. Round not closed. Baseline file not edited (orchestrator).
- Pin: `647c6a2a78257743db42b25019fd02fdf376618c`
- Slice: MIG3b ticket 01

## Host access check

Production `app.X` uses (tests excluded). Replacement is what ticket 03 can call. A gap means ticket 03 cannot keep today's behaviour from `Host` alone.

| Call | Where | Host replacement | Gap |
| --- | --- | --- | --- |
| `app.Logger` Info/Warn/Error | `plugin.go` (media missing), `sync_obs.go`, `sync_prepare_production.go` (fallback when the job logger is nil) | `Host.Logger()` — same Info/Warn/Error. `app.Logger` already satisfies `pluginsdk.Logger` | none |
| `app.Config == nil` | `plugin.go` Init | no object. `Host.ConfigValue` returns `""` when unset | no nil-config signal. Init's "config not configured" error has nothing to call |
| `app.Config` settings | already `SettingsFrom(h.ConfigValue)` | `Host.ConfigValue` | none |
| `queue.driver` | `pluginsdk.RequireQueueDriver` | `Host.PlatformConfig("queue.driver")` | none |
| `app.Bootstrap.DB` | `plugin.go` (templates, uploads, designs, generate), prepare reclaim SQL | `Host.BootstrapDB()` `(*sql.DB, bool)` after `Require(CapDB)` | none |
| `app.BoundQueue()` | generate enqueue, prepare enqueue, health | no method. Enqueue is `pluginsdk.SyncJobs.Enqueue`. Read/requeue is `Host.Jobs()` | `Host.Jobs()` is `app.JobsPort()` and only when `CapJobs` is bound. `BoundQueue` is set by `BindQueue` without `BindJobsPort`. Public health is OK in that state (`TestHealth_UsesBoundQueueWithoutRegisterQueue`) |
| `app.SyncJobs()` | health job-type list; enqueue finds the registration | no list method. Register/enqueue is `pluginsdk.SyncJobs` (`Integration(slug).RegisterManual` / `Enqueue`) | no way to list registered types. Admin health walks `SyncJobs()` |
| `queue.Enqueue` + `NewSyncJobEnvelope` + `InjectRequestID` | generate and prepare | `SyncJobs.Enqueue(ctx, name, payload, WithJobID(id))` injects the request id and builds the envelope | see conflict semantics below. Ticket 01 still uses the envelope path |
| `Reader.Get` / `Queue.Requeue` | generate conflict, prepare conflict | `Host.Jobs().Get` / `Requeue` (`pluginsdk.WithRequeueMinAge`, `WithRequeueTerminal`, same sentinels) | none for these two methods |
| `Reader.CountsByType` | admin health depths | `Host.Jobs()` has `CountsByStatus()` only (all types together) | **`JobsPort.CountsByType` is missing.** Admin health reports pending/processing/failed per registered type. `CountsByStatus` cannot fill that table |
| `jobs.Reader` type assert | public health | `Host.Jobs() (JobsPort, bool)` | not the same check. See `BoundQueue` row |
| `app.MediaStorage()` | `assets.go` during Init | `Host.Media()` | **`Host.Media` reads `BoundMediaStorage()`.** Init runs before `BindMediaStorage`, which is why `assets.go` calls `MediaStorage()`. `AsSignedURLStorage` exists on `MediaPort` (not a signed-URL gap) |
| `app.Hooks(name).Register` | `cart_validate.go` | `Host.Hooks(name).Register` | none |
| `Host.OnRequeued` | `job_fence.go` (already on Host) | `Host.OnRequeued` | none |
| `pluginsdk.ErrSkipOutcome` | generate worker | same sentinel as `jobs.ErrSkipOutcome` | none |

Ticket 01 is not blocked: enqueue, get, requeue, and per-type counts are still on the bound queue, and the adapter type-asserts them. The two symbols that block a later flip of health and media, without a Shopanda change, are `JobsPort.CountsByType` and `Host.Media` using `BoundMediaStorage` during Init. There is also no Host list of registered sync jobs.

## Conflict semantics (do not collapse in ticket 03)

`WithJobID` on conflict: same sync type and pending/processing/reclaiming → success; failed/done/cancelled → `Requeue(WithRequeueTerminal)`; wrong type → error. No min-age.

Generate, unchanged in this ticket: stale processing → `Requeue(WithRequeueMinAge(domain.DefaultClaimLease))`; too-fresh processing → success; not processing → terminal requeue; reclaiming → error. Fence stays `OnRequeued`.

Prepare, unchanged: failed/done/pending → success and no `Requeue`; young processing → left; stale processing and cancelled → type-scoped SQL (`attempts` unchanged on stale processing, `attempts = 0` on cancelled). Not `Admin.Retry`.

## Job ids and UUID

`WithJobID` rejects a non-UUID. Production generate and prepare ids are UUIDs: `newGenerateServices` and `newDesignService` set `IDs: ports.UUIDGenerator{}`, and `UUIDGenerator.New` is `uuid.NewString()` (canonical v4, `ports/ids_test.go`). Generate uses that id as the queue id (`application/generate.go`). Prepare uses the design-version id from the same generator (`application/approve.go`).

Unit conflict tests still pass non-UUIDs (`job-1`, `ver-1`) through `EnqueueManual`, which sets `job.ID` on the envelope and does not call `WithJobID`.

## Ticket 01

| Was | Now |
| --- | --- |
| `enqueueGenerateIdempotent` / `enqueuePrepareProduction` / `Health` / `AdminHealth` take `*plugin.App` | `ports.JobAccess` / `ports.JobHealth`. `PlatformJobs(app)` adapts `*plugin.App` |
| `sync_generate.go`, `sync_prepare_production.go`, `httpapi/health.go`, `application/generate_worker.go` import `internal/domain/jobs` | gone. Status constants are `ports.JobStatus*`. Skip sentinel is `pluginsdk.ErrSkipOutcome` (same value) |
| register helpers take `*plugin.App` | `*pluginsdk.SyncJobs` (and a `pluginsdk.Logger` for the prepare fallback) |

C4 assertions were not edited. Call sites that built a `*plugin.App` now pass `PlatformJobs(app)` or `pluginsdk.New(app, PluginName).Integration(PluginName)`.

## Reader check vs `jobs.Reader`

`ReaderBound` and `CountsByType` both assert `queueReaderSurface`: `Get`, `CountsByStatus`, `CountsByType`. The old health check was `q.(jobs.Reader)`, which also requires `List`. A bound queue with those three methods and no `List` now reports public health ok. The Postgres queue implements `List`, so a real queue still passes or fails as before.

`List` is not on the surface. `pluginsdk.JobsPort` does not expose it, and there is no exported `ListFilter` or `Summary`. Putting `List` on `queueReaderSurface` means `plugin.go` imports `internal/domain/jobs`. That line is not in the ImportBoundary baseline, and the gate fails on any line the baseline does not already name. Baseline file is not edited in this ticket.

## ImportBoundary

`go test ./plugins/ -run TestImportBoundary` on 2026-11-20: 14 → 7. Baseline file not edited (orchestrator).

Still present:

- `metaldogs/adapters/mediastore/store.go` → `internal/domain/media`
- `metaldogs/assets.go` → `internal/domain/media`
- `metaldogs/assets.go` → `internal/platform/plugin`
- `metaldogs/cart_validate.go` → `internal/platform/plugin`
- `metaldogs/migrations.go` → `internal/platform/plugin`
- `metaldogs/plugin.go` → `internal/platform/plugin`
- `metaldogs/sync_obs.go` → `internal/platform/plugin`

No longer present (still listed in the baseline): `generate_worker.go`, `httpapi/health.go`, `sync_generate.go`, and `sync_prepare_production.go` (`internal/domain/jobs` and `internal/platform/plugin`).

## Tests touched (assertions unchanged)

- `sync_generate_test.go` — register and enqueue call sites
- `sync_prepare_production_test.go` — handler, register, enqueue, `resolvePrepareEnqueueConflict(nil, store, …)` where the old app had no DB
- `httpapi/health_test.go` — `Health` / `AdminHealth` take `PlatformJobs(app)`

## Not in this change

No `Init(Host)`, no `pluginsdk.Adapt`, no `WithJobID` call, no edits under `internal/*`, `pkg/*`, or core `cmd/api/*`. Baseline file not edited.
