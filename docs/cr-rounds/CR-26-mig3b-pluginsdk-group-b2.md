# CR-26: MIG3b pluginsdk group B2 (tickets 01–04)

- Status: closed 2026-11-21. Baseline file not edited in this repo (orchestrator removes it and sets the gate scripts strict after review).
- Pin: `d9ccc21c…`
- Slice: MIG3b tickets 01–04

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

## Host.Media gaps (ticket 02)

`ports.MediaBackend` is the surface `mediastore` wraps: `Name`, `Save`, `Delete`, `URL`, `Head`, `SignedURL(ctx, path, pluginsdk.SignURLOptions)`. `*plugin.App` does not have that method set. `openAssetStore` still calls `MediaStorage()` and type-asserts `ports.MediaBackend` (same methods as `media.SignedURLStorage`, with `pluginsdk.SignURLOptions` standing in for `media.SignURLOptions`). `Read` and `SaveWithContentType` stay optional asserts on the concrete value.

| Gap | SDK today | Decision |
| --- | --- | --- |
| `AsReadableStorage` | `MediaPort.AsReadableStorage` returns the concrete `ReadableStorage` (`Read` only) | Ticket 02 asserts `Read` on the concrete backend. Same signature as `pluginsdk.ReadableMediaStorage`. Ticket 03 can use `AsReadableStorage`, or assert `Read` on the value from `AsSignedURLStorage` (localfs and S3 implement both). No upstream change |
| `AsSignedURLStorage` | `MediaPort.AsSignedURLStorage` returns the concrete signer as `SignedURLMediaStorage` (`Head` + `SignedURL` only, not `Save`) | Ticket 02 replaces `media.AsSignedURLStorage` with the `ports.MediaBackend` assert on `MediaStorage()`. Ticket 03 must assert that returned value to `ports.MediaBackend`, not call `MediaPort.Save`. `AsSignedURLStorage` returns the backend itself, so `Save` / `Delete` / `URL` are still on the dynamic type. No upstream change |
| `SignURLOptions` | `pluginsdk.SignURLOptions` is an alias of `media.SignURLOptions` (`TTL`, `Filename`, `Disposition`) | Adapter uses the alias. No missing field |
| dispositions | `MediaDispositionAttachment` / `MediaDispositionInline` alias `media.DispositionAttachment` / `DispositionInline` | Adapter sets those constants (`attachment` / `inline`). `NormalizeMediaDisposition` is not required; the value is explicit. No gap |

`SaveWithContentType` is not on `MediaPort` or `SignedURLMediaStorage`. It still runs in ticket 02 because the assert sees the concrete backend (`localfs` ignores the MIME; S3 stores it). Ticket 03 drops MIME on S3 if it calls `MediaPort.Save` instead of the concrete value. Decision: keep the assert. Do not patch upstream.

`Host.Media()` reads `BoundMediaStorage`, which is unset during `Init`. `openAssetStore` stays on `MediaStorage()` so the eval-only branch (no customizer HTTP) still happens at `Init`. The agreed lazy wrapper is ticket 03. That branch is Init-time: ticket 03 stops if a lazy `Host.Media()` call cannot keep "no media → eval CLI only, no HTTP".

## Ticket 02

| Was | Now |
| --- | --- |
| `resolveAssetStore(*plugin.App)` imported `domain/media` and called `media.AsSignedURLStorage` | `resolveAssetStore(ports.MediaBackend)`. `openAssetStore` adapts `*plugin.App` |
| `mediastore.New(media.SignedURLStorage)` | `mediastore.New(ports.MediaBackend)`. Key rejection, content type, ctx cancel unchanged. `adapters/mediastore` tests not edited |
| `registerObsPing(*plugin.App)` | `syncRegistrar` + `pluginsdk.Logger`. `*pluginsdk.SyncJobs` and `app.Logger` satisfy them |
| `registerCartValidation(*plugin.App)` | `pluginsdk.HookAPI`. `app.Hooks(name)` satisfies it |

## ImportBoundary

`go test ./plugins/ -run TestImportBoundary` on 2026-11-20 after ticket 02: 7 → 2. Baseline file not edited (orchestrator). No new line.

Still present:

- `metaldogs/migrations.go` → `internal/platform/plugin`
- `metaldogs/plugin.go` → `internal/platform/plugin`

No longer present (still listed in the baseline): `assets.go` (`domain/media`, `platform/plugin`), `adapters/mediastore/store.go` (`domain/media`), `cart_validate.go`, `sync_obs.go`.

## Tests

`TASK_DB=shopanda_task_mig3b bash ~/ai-os/config/tests/metal_dogs.sh` on 2026-11-20: pass (`./plugins/metaldogs/...`, `./cmd/api/...`, gofmt, deviations, docs links). ImportBoundary line from that run: `2 znanych naruszeń (baseline), 0 nowych`. `adapters/mediastore` tests were not edited.

## Not in this change

No `Init(Host)`, no `pluginsdk.Adapt`, no edits under `internal/*`, `pkg/*`, or core `cmd/api/*`. Baseline file not edited.

## Ticket 03

`Plugin.Init` takes `pluginsdk.Host`. `cmd/api/register_products.go` registers `pluginsdk.Adapt(metaldogs.New())`. `Migration` returns `pluginsdk.MigrationSpec`. `Needs` / `Require`: `CapCart`, `CapCatalog`, `CapCatalogSeed`, `CapDB`, `CapJobs`, `CapMedia`. The nil-config check is gone.

`Host.Media()` is unset during `Init` (bind is after `InitAll`). The plugin always registers customizer routes and sync jobs, and stores a wrapper that calls `Host.Media()` on each use (`TestPlugin_Init_WithoutMedia_RoutesRegisteredMediaResolvedLazily`). `AsSignedURLStorage` is asserted to `ports.MediaBackend` at first use, not at Init. A backend that is not `SignedURLStorage` errors with `does not implement SignedURLStorage` (`TestPlugin_UnsignedMediaFailsOnUseHealthOmitsIt`). A value returned by `AsSignedURLStorage` that is not `ports.MediaBackend` errors with `does not implement metaldogs MediaBackend`. Core `media.SignedURLStorage` includes `Save`/`Delete`/`URL`, so localfs and S3 do not hit that second string. Unbound media fails the first use. Init no longer refuses to mount routes when the backend cannot sign URLs. `/api/customizer/health` does not read media: with no queue it is degraded, and it does not mention either media error. The first asset call returns the error. That is a loss of the old F4 fail-closed mount. Eval stays `RequiresDB: false`.

Enqueue is `SyncJobs.Enqueue(..., WithJobID)`. Generation and design-version ids are `ports.UUIDGenerator` (UUID). A non-UUID id fails closed (`TestEnqueueGenerate_RejectsNonUUIDJobID`). After a successful generate enqueue, a processing row older than `DefaultClaimLease` is requeued with `WithRequeueMinAge`. Fresh processing is left (`TestEnqueueGenerate_LeavesFreshProcessing`). Terminal requeue, including one `ErrRequeueWrongAttempt` retry and `ErrRequeueNotTerminal` as success, stays inside the pin (`requeueTerminalSyncJob`, PR-1595). The plugin does not retry `Enqueue`. Two concurrent enqueues of one failed generate job both return nil and requeue once (`TestEnqueueGenerate_ConcurrentTerminalBothNilOneRequeue`; the fence claim moves by 1). A row already `reclaiming` when `WithJobID` reads it returns nil (`TestEnqueueGenerate_ReclaimingRowIsSuccess`). That name used to say "conflict"; the plugin no longer has a generate conflict path. Prepare still applies its own rule when `Get` finds a row (failed and done stay, cancelled and stale processing use the SQL reclaim). `WithJobID` runs only when that row is absent. That check is not atomic. `ApproveBound` serializes the version insert, but the winner, the conflict loser (`approveIdempotent`), and a later approve of an already-approved design all call `ensurePrepareEnqueued` with no lock around the queue row. A row that is absent at `Get` and terminal at `Enqueue` is requeued by `WithJobID` (`TestEnqueuePrepare_TerminalRowAfterNilGetIsRequeued`). The plugin cannot put it back: a done row requeued with attempts kept at 0 looks like a fresh insert. The pin has no enqueue option that returns conflict on a terminal row instead of `Requeue(WithRequeueTerminal)`. That option is the close for this window; it is not patched here.

Admin health types come from the names the plugin registered (`obs.ping`, `prepare_production_artwork`, `generate` when the worker exists), turned into types with `extapi.SyncJobType`. Depths use `Host.Jobs().CountsByType`. Public health is ok only when `Host.Jobs()` is bound (`CapJobs`).

`cmd/api/plugin_boot.go` is the pin (not edited). `pluginCLINeedsSkip` skips `CapJobs` and `CapMedia` (PR-1594). Plugin CLI does not bind those ports, so `Host.Jobs()` and `Host.Media()` stay unset there. `Require(CapJobs)` stays so serve and worker still declare the port; the CLI assert ignores it. `metaldogs:eval` is `RequiresDB: false`.

Changed tests: `plugin_test.go` boots through `Adapt`; without bound media the routes and sync jobs are still registered and `AssetStore.Head` errors. Health tests bind `JobsPort` and pass the job name list (`TestHealth_UsesBoundQueueWithoutRegisterQueue` binds queue and port). Fresh processing is `TestEnqueueGenerate_LeavesFreshProcessing` (SDK returns nil; the plugin does not requeue). An existing prepare row is `TestEnqueuePrepare_ExistingPendingRowSkipsEnqueue` (plugin `Get` before `Enqueue`). Repend tests bind `JobsPort` on the wrapped queue. `TestEnqueueGenerate_SameIDStaleAndFresh` covers one id, a terminal requeue, a stale processing requeue, and a fresh processing row left alone. `TestEnqueueGenerate_ConcurrentTerminalBothNilOneRequeue` is the pin's terminal race through the plugin. Cart harness still expects no design service when `Bootstrap.DB` is unset. Boot fixture binds a jobs port and media so serve assert passes.

`go test ./plugins/ -run TestImportBoundary` on 2026-11-20: pass. Production violation count: 0. The gate skips `*_test.go`. Test files that import `internal/*` (not counted):

- `cmd/api/register_plugins_metaldogs_test.go` — `internal/domain/identity`, `internal/domain/media`, `internal/domain/rbac`, `internal/interfaces/http/shared`, `internal/interfaces/http/storefront`, `internal/platform/auth`, `internal/platform/cli`, `internal/platform/config`, `internal/platform/db`, `internal/platform/event`, `internal/platform/logger`, `internal/platform/migrate`, `internal/platform/plugin`
- `plugins/metaldogs/adapters/mediastore/store_test.go` — `internal/domain/media`, `internal/infrastructure/localfs`
- `plugins/metaldogs/application/generate_test.go` — `internal/domain/jobs`
- `plugins/metaldogs/cart_add_test.go` — `internal/domain/catalog`, `internal/domain/inventory`, `internal/domain/pricing`, `internal/domain/shared`, `internal/infrastructure/postgres`, `internal/platform/id`
- `plugins/metaldogs/cart_harness_test.go` — `internal/application/cart`, `internal/application/composition`, `internal/application/extension`, `internal/application/hooks`, `internal/application/pricing`, `internal/domain/inventory`, `internal/domain/pricing`, `internal/infrastructure/postgres`, `internal/interfaces/http/shared`, `internal/interfaces/http/storefront`, `internal/platform/config`, `internal/platform/event`, `internal/platform/logger`, `internal/platform/plugin`
- `plugins/metaldogs/cart_place_test.go` — `internal/platform/apperror`, `internal/platform/pluginwrite`
- `plugins/metaldogs/cart_validate_test.go` — `internal/platform/apperror`, `internal/platform/pluginwrite`
- `plugins/metaldogs/catalog_seed_test.go` — `internal/application/composition`, `internal/domain/catalog`, `internal/domain/shared`, `internal/domain/store`, `internal/domain/tax`, `internal/infrastructure/postgres`, `internal/interfaces/http/shared`, `internal/interfaces/http/storefront`, `internal/platform/cli`, `internal/platform/id`, `internal/platform/logger`, `internal/platform/plugin`
- `plugins/metaldogs/httpapi/designs_test.go` — `internal/domain/identity`, `internal/interfaces/http/storefront`, `internal/platform/auth`
- `plugins/metaldogs/httpapi/health_test.go` — `internal/domain/jobs`, `internal/platform/config`, `internal/platform/plugin`
- `plugins/metaldogs/job_fence_notx_test.go` — `internal/platform/plugin`
- `plugins/metaldogs/job_fence_race_test.go` — `internal/domain/jobs`, `internal/infrastructure/postgres`, `internal/platform/plugin`
- `plugins/metaldogs/job_fence_test.go` — `internal/domain/jobs`, `internal/platform/plugin`
- `plugins/metaldogs/observability/correlation_test.go` — `internal/platform/requestctx`
- `plugins/metaldogs/owner_token_auth_test.go` — `internal/application/auth`, `internal/domain/customer`, `internal/domain/identity`, `internal/interfaces/http/storefront`, `internal/platform/auth`, `internal/platform/jwt`, `internal/platform/jwt/jwttest`
- `plugins/metaldogs/plugin_test.go` — `internal/application/hooks`, `internal/domain/extension`, `internal/domain/media`, `internal/domain/rbac`, `internal/platform/cli`, `internal/platform/config`, `internal/platform/event`, `internal/platform/logger`, `internal/platform/plugin`
- `plugins/metaldogs/ports/assetstore_test.go` — `internal/domain/media`
- `plugins/metaldogs/settings_test.go` — `internal/platform/config`, `internal/platform/plugin`
- `plugins/metaldogs/sync_generate_repend_test.go` — `internal/domain/jobs`, `internal/infrastructure/postgres`, `internal/platform/plugin`
- `plugins/metaldogs/sync_generate_test.go` — `internal/domain/jobs`, `internal/platform/apperror`, `internal/platform/plugin`
- `plugins/metaldogs/sync_obs_test.go` — `internal/domain/jobs`, `internal/platform/logger`, `internal/platform/plugin`, `internal/platform/requestctx`
- `plugins/metaldogs/sync_prepare_production_test.go` — `internal/domain/jobs`, `internal/platform/apperror`, `internal/platform/logger`, `internal/platform/plugin`
- `plugins/metaldogs/testutil/core_schema_test.go` — `internal/application/composition`, `internal/infrastructure/postgres`, `internal/interfaces/http/storefront`
- `plugins/metaldogs/upload_wire_test.go` — `internal/platform/logger`

Baseline file not edited. Production `plugins/metaldogs` has no `internal/platform/plugin`, `internal/domain/jobs`, or `internal/domain/media` import.

## Ticket 04

Production code already implements `pluginsdk.Plugin`. This ticket records that shape and the empty import boundary. Gate scripts in `~/ai-os/config/tests` are not edited here. The baseline file is not edited here.

### What moved

| Was | Now |
| --- | --- |
| `Plugin.Init(*plugin.App)` | `Plugin.Init(pluginsdk.Host)` |
| `registry.Register(metaldogs.New())` on a `plugin.Plugin` | `registry.Register(pluginsdk.Adapt(metaldogs.New()))` in `cmd/api/register_products.go` |
| `plugin.Migrator` / `plugin.MigrationSpec` | `pluginsdk.Migrator`; `Migration` returns `pluginsdk.MigrationSpec` |
| enqueue via `BoundQueue` + `NewSyncJobEnvelope` | `SyncJobs.Enqueue(ctx, name, payload, WithJobID(id))` |
| `domain/jobs` and `domain/media` in production files | `Host.Jobs()`, `Host.Media()`, `ports` surfaces |

`plugins/metaldogs` production `.go` files (not `*_test.go`) import no `internal/*`. `TestImportBoundary` skips `*_test.go`. Test files that still import `internal/*` are listed under ticket 03.

### Decisions per gap

| Gap | Decision |
| --- | --- |
| `JobsPort.CountsByType` was missing | Shopanda added it. Admin health calls `Host.Jobs().CountsByType`. No Metal Dogs patch under `pkg/` or `internal/` |
| `Host.Media()` unset during `Init` | Lazy wrapper. Routes and sync jobs register at Init. The first asset use asserts `AsSignedURLStorage` to `ports.MediaBackend`. A missing or unsigned backend fails that call |
| No Host list of registered sync jobs | Plugin keeps the names it registered and derives types with `extapi.SyncJobType` |
| `BoundQueue` can exist without `JobsPort` | `Require(CapJobs)`. Serve and worker bind queue and port together. Public health is ok only when `Host.Jobs()` is bound. `TestHealth_UsesBoundQueueWithoutRegisterQueue` binds both |
| No `Host` equivalent of `app.Config == nil` | Check dropped. `Host.ConfigValue` returns `""` when unset |
| `WithJobID` treats pending and processing as success and requeues only terminal rows | Kept. After generate `Enqueue`, the plugin `Get`s the row and requeues processing older than `DefaultClaimLease` with `WithRequeueMinAge`. Fresh processing is left. Prepare still applies its own rule when `Get` finds a row (failed and done stay; cancelled and stale processing use the type-scoped SQL). That prepare check is not atomic; recorded in ticket 03 |
| Job id must be a UUID | Generate and prepare ids come from `ports.UUIDGenerator` (`uuid.NewString()`). A non-UUID generate id fails closed |

### Stale-claim rule

Unchanged. A generate row in `processing` whose `UpdatedAt` is older than `domain.DefaultClaimLease` is requeued with `pluginsdk.WithRequeueMinAge(domain.DefaultClaimLease)`. A younger processing row is left. `ErrRequeueTooFresh` is success. The fence stays `Host.OnRequeued`. C4 assertions were not weakened in tickets 01–03 and are not edited here.

### Docs

`plugins/metaldogs/README.md`, `AGENTS.md`, and `docs/architecture/overview.md` describe `pluginsdk.Plugin` and `pluginsdk.Adapt`. README no longer says Init without media registers eval only, and no longer names `App.BoundQueue` as the enqueue API. `docs/ROADMAP.md` verify rows that said `WithJobID` and `CountsByType` are unused, and that migrations implement `plugin.Migrator`, now match this pin.

### Tests

Plain `go test -count=1 ./plugins/ -run TestImportBoundary -v` on 2026-11-21: `TestImportBoundary` PASS, `TestImportBoundary_ForbidsCorePluginImport` PASS, no violation lines. The test has no baseline file; any production `internal/*` import fails it.

`bash .github/scripts/check-upstream-deviations.sh`: `upstream-deviations OK (pin d9ccc21c48ea, 17 allowlisted path(s))`.

`bash .github/scripts/check-product-docs-links.sh`: pass.

`TASK_DB=shopanda_task_mig3b_flip bash ~/ai-os/config/tests/metal_dogs.sh` on 2026-11-21: first run exit 1. `plugins/metaldogs/application` hit the 10m timeout in `TestGenerateWorker_LostClaimAfterGenerate_SkipsComplete`: `gatedProvider.Generate` took the `default` branch on an unbuffered `gate` send, so the test blocked on `<-gate` and the provider blocked on `<-release`. No product code changed in this ticket. Retry of the same command: exit 0. `plugins/metaldogs/application` 0.025s ok. Summary lines: `gofmt: zmienione pliki OK`, `ImportBoundary: 0 naruszeń (strict)`, `upstream-deviations OK (pin d9ccc21c48ea, 17 allowlisted path(s))`, `deviations + docs-links OK`.

### Closure checklist

- [x] `docs/ROADMAP.md`: verify rows for the import boundary, `WithJobID`, `CountsByType`, and `pluginsdk.Migrator` match the flip. MIG3b is not a named slice in the roadmap.
- [x] `architecture/overview.md` states `pluginsdk.Plugin` + `Adapt`. Domain model and flows were not changed by this round.
- [x] API paths, DTOs, and error codes are unchanged. Health is still public `degraded` / 503 when `Host.Jobs()` is unbound. `architecture/api.md` and the generation-job runbook name that check `Host.Jobs()` bound (`CapJobs`). Admin depths are `Host.Jobs().CountsByType`. ROADMAP F3 marks `App.BoundQueue()` unused by the plugin after MIG3b. The HTTP contract is the same.
- [x] No new ADR. Gap decisions are the table above. ADR-0001 still holds: Metal Dogs is an external plugin.
- [x] No new job type, flag, or failure mode. The generate stale-claim rule is the one already in the C4 runbook path (`Host.OnRequeued` plus `WithRequeueMinAge`).
- [x] `plugins/metaldogs/README.md` and `AGENTS.md` describe the plugin shape. `storefront/README.md` and `docs/README.md` are outside this flip.
- [x] Tests: no new test in this ticket. Safety net is the existing suite plus `TestImportBoundary` with zero production violations. Regression coverage for the flip is the ticket 03 tests named above.
- [x] Safe delivery: plugin flag still defaults off. No migration. Rollback is reverting the branch; the pin is unchanged.
- [x] Privacy/cost: no new logs, no new provider calls.
- [x] This record is linked from the index.

Real `go run ./cmd/api` on 2026-11-21, DB `shopanda_task_mig3b_flip` port 5433, plugin enabled, `cmd/api/plugin_boot.go` equal to the pin: `metaldogs:templates validate -file plugins/metaldogs/templates/head-front-blackmetal.yaml` printed `ok slug=head-front-blackmetal hash=e66b2a96d1ca5bb7d671e088ccebb57db40ec9c247b6073fe79ea11bbe1ec21f` and exited 0. `metaldogs:eval -provider fake -dog-name Burek` wrote 1 ok / 0 failed and exited 0. Core postgres search/cache/queue still log `database not configured` at Init; Metal Dogs init completed. That log is from before the CLI opens the pool. `TestRunPluginCLICommand_TemplatesUsesBoundDB`, `TestRunPluginCLICommand_CatalogSeedWritesProductAndVariant` (`metaldogs:catalog-seed: ok`), `TestRunPluginCLICommand_EvalRunsWithoutDSN`, and `TestMetalDogs_ServeAndWorkerBootWithoutCatalogSeed` passed on the same DSN. `go test ./plugins/metaldogs/... -count=1 -p 1` passed. `go test ./plugins/ -run TestImportBoundary` passed with no violation lines (production count 0). Baseline file not edited.
