# CR-21: Shopanda pin `7c982b00` (UPG4 ticket 00)

- Status: pin merged; later UPG4 tickets still delete deviations
- Date: 2026-11-17
- Slice: UPG4 ticket 00
- Base: `3593795d` (`origin/main` after PR #40)
- Pin: `7c982b00e40d4389ad7e5f211831101963272154` (`upstream/main`, PR #606)

## Conflict resolution

Kept Metal Dogs where the file is a listed deviation. `pkg/pluginsdk/*_test.go` took upstream (they match the pin). Other conflicts took upstream. `docs/` additions from Shopanda were not vendored.

| File | Kept |
| --- | --- |
| `AGENTS.md`, `PLUGINS.md`, `RUNBOOK.md` | Metal Dogs. Blob/tree links rewritten to the new pin |
| `cmd/api/bind_queue_test.go`, `jobs_cli.go`, `jobs_cli_test.go`, `main.go`, `wire_routes.go` | Ours. `jobs_cli_test.go` was restored from HEAD after a bad auto-merge (upstream helpers `newJobsRuntime` / `newJobsIntrospection` are not on our CLI) |
| `cmd/api/register_plugins.go` | Ours `registerMetalDogs` plus upstream `registerProductPlugins` |
| `configs/config.example.yaml` | Metal Dogs `plugins.metaldogs` block plus upstream Extra-config comment |
| `internal/platform/config/config.go` | Metal Dogs `generation.*` env/flatten plus upstream `applyPluginsExtraEnv` / `flattenPluginsExtra` |
| `internal/application/integration/sync_runtime.go` (+ tests), `sync_manual_roundtrip_test.go` | Ours |
| `internal/domain/jobs/worker.go`, `worker_test.go` | Ours. Test double gained `Requeue` so it still implements `jobs.Queue` |
| `internal/domain/jobs/job.go` | Ours `Complete`/`Fail(ctx, id, attempts)` and `ErrSkipOutcome` text `job: skip outcome`. Added upstream `Queue.Requeue(ctx, id, attempt)`, `ErrOutcomeNotApplied`, `ErrRequeueConflict`, `ErrRequeueUnsupported`, `DefaultRequeueMinAge` so merged drivers compile |
| `internal/domain/jobs/admin.go` | Ours lease reclaim, renamed `Requeue` → `RequeueLease`. `Queue.Requeue(ctx, id, attempt) error` and `Admin.Requeue(ctx, id, lease) (bool, error)` cannot both exist on `*postgres.JobQueue` |
| `internal/platform/plugin/providers.go` (+ test), `sync_job_test.go` | Ours, plus upstream `Bootstrap.Prices` / `PriceHistory` / `Stock` (required by merged `plugin_ports.go`) |
| `internal/platform/runtime/scheduler_test.go` | Ours, plus `Requeue` stub |
| `cmd/api/job_queue_bind.go` | Ours (no `WrapRequeueNotify` before `BindQueue`). Auto-merge had inserted the wrap; `BindQueue` still requires the registered instance, and the generate fence type-asserts the Postgres queue |
| `internal/platform/plugin/queue_hooks.go` (+ test) | Upstream, plus `RequeueLease` forwarder. Test fake implements `RequeueLease` so it still counts as `jobs.Admin` |
| `internal/infrastructure/postgres/job_queue.go` (+ test) | Ours (`SetOnRequeued`, `RequireOnRequeued`, `RequeueLease`). Added upstream `Requeue(ctx, id, attempt)` |
| `internal/infrastructure/{kafka,rabbitmq,redis,sqs}/job_queue.go`, `redis/job_queue_test.go` | Upstream |
| `pkg/pluginsdk/sdk_test.go`, `pkg/pluginsdk/harness/harness_test.go` | Upstream (identical to the pin) |
| `plugins/example/plugin_test.go` | Upstream |
| importer/inventory/search/webhook/search-admin tests | Upstream |
| Shopanda guide files under docs/guides (DEVELOPER, EXTENSION_POINTS, PLUGIN_RUNTIME_CONTRACT, PRODUCT_SITES, SYNC_JOB_OBSERVABILITY) | Deleted (modify/delete). Stub index stays |
| `docs/phase-17-product-packaging/**`, `docs/runbooks/product-fork-upgrade.md` | Deleted |

## Fence compile adaptation

- `plugins/metaldogs/sync_generate.go` calls `admin.RequeueLease`.
- `plugins/metaldogs/job_fence.go` still uses `SetOnRequeued` / `RequireOnRequeued` on the Postgres queue. That API is not on the pin; the file was kept so the fence still installs.
- Cart test doubles (`qtyCart`, `gateCart`) implement `DiscardEmptyGuestCart` as a no-op. Production add-to-cart does not call it yet.
- Queue test doubles that we kept gained `Requeue(ctx, id, attempt) error`.

## Upstream range `a411c970..7c982b00` touching files we use

- Cart: `pending_line`, `DiscardEmptyGuestCart`, `CatalogVariant.ProductVisible`, `ClassifyProductSlug` (`pkg/extapi`, `pkg/pluginsdk/ports.go`, `internal/application/cart`).
- `App.BoundQueue`, sync handler `request_id` (`internal/platform/plugin`, `internal/application/integration/sync_runtime.go`).
- `cmd/api/register_products.go` and `registerProductPlugins`.
- `pluginsdk.NoCache` on `RegisterPublicRoute`.
- `Config.Extra` / `SHOPANDA_PLUGINS_<SLUG>_*` (`internal/platform/config`).
- `CatalogSeedPort` (`pkg/pluginsdk`, `cmd/api/plugin_ports.go`). Not used by Metal Dogs seed (ticket 02 deferred).
- `jobs.Queue.Complete`/`Fail(ctx, id, attempt)`, `ErrSkipOutcome`, `Queue.Requeue`, `Host.OnQueueBound`, `Host.OnRequeued`.
