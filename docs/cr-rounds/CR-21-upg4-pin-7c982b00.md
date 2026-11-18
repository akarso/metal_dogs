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
| `cmd/api/job_queue_bind.go` | Ours (no `WrapRequeueNotify` before `BindQueue`). Auto-merge had inserted the wrap; `BindQueue` still requires the registered instance, and the generate fence type-asserts the Postgres queue. In this tree `Host.OnQueueBound` and `Host.OnRequeued` register hooks that never run, and `jobs:requeue` is absent, until ticket 03 restores upstream `job_queue_bind.go` and `jobs_cli.go` |
| `internal/platform/plugin/queue_hooks.go` (+ test) | Upstream, plus `RequeueLease` forwarder. Test fake implements `RequeueLease` so it still counts as `jobs.Admin` |
| `internal/infrastructure/postgres/job_queue.go` (+ test) | Ours (`SetOnRequeued`, `RequireOnRequeued`, `RequeueLease`). Added upstream `Requeue(ctx, id, attempt)`. It refuses `MetalDogsGenerateJobType` and `RequireOnRequeued` types: the bare UPDATE skipped `runOnRequeued`. Ticket 03 must replace that refusal with the in-transaction requeue. The pin query casts min-age as `$3::text`; the milliseconds argument is passed as a string so pgx can encode it |
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
- `Host.OnQueueBound` and `Host.OnRequeued` do nothing here: `cmd/api` never calls `ApplyQueueBoundHooks` or `WrapRequeueNotify`. `jobs:requeue` is missing because `jobs_cli.go` stayed ours. Ticket 03 restores upstream `job_queue_bind.go` and `jobs_cli.go`.
- Until that lands, `postgres.JobQueue.Requeue` returns an error (not `ErrRequeueConflict`) for `MetalDogsGenerateJobType` and every `RequireOnRequeued` type and leaves the row processing. `RequeueLease` is unchanged and still runs the fence.

## Upstream range `a411c970..7c982b00` touching files we use

- Cart: `pending_line`, `DiscardEmptyGuestCart`, `CatalogVariant.ProductVisible`, `ClassifyProductSlug` (`pkg/extapi`, `pkg/pluginsdk/ports.go`, `internal/application/cart`).
- `App.BoundQueue`, sync handler `request_id` (`internal/platform/plugin`, `internal/application/integration/sync_runtime.go`).
- `cmd/api/register_products.go` and `registerProductPlugins`.
- `pluginsdk.NoCache` on `RegisterPublicRoute`.
- `Config.Extra` / `SHOPANDA_PLUGINS_<SLUG>_*` (`internal/platform/config`).
- `CatalogSeedPort` (`pkg/pluginsdk`, `cmd/api/plugin_ports.go`). Not used by Metal Dogs seed (ticket 02 deferred).
- `jobs.Queue.Complete`/`Fail(ctx, id, attempt)`, `ErrSkipOutcome`, `Queue.Requeue`, `Host.OnQueueBound`, `Host.OnRequeued`.

## Ticket 01 — registration, Extra config, NoCache

Moved:

- `registerMetalDogs` from `cmd/api/register_plugins.go` to `registerProductPlugins` in `cmd/api/register_products.go`. `register_plugins.go` and `wire_routes.go` match the pin. Deleted `cmd/api/cache_control_prefixes.go` (+ test).
- Typed `MetalDogsPluginConfig` / `Plugins.MetalDogs` removed from `internal/platform/config/config.go`. `plugins_extra.go` no longer lists `metaldogs` in `typedPluginYAMLKeys`, so the slug is `Config.Extra`. Both files still differ from the pin (the pin keeps the typed `enabled` field). `config_test.go` matches the pin again.
- Public `/api/customizer/*` routes use `pluginsdk.NoCache()`. `pathPrefixFromPattern` does not cut at `{`, so `GET /api/customizer/designs/{id}` is not a prefix. `GET /api/customizer` → `http.NotFoundHandler()` supplies `/api/customizer`. Exact `GET /api/customizer` is a plain 404. Guest GET and HEAD are `no-store` through `CacheControlMiddleware`.
- `job_queue_bind.go` and `jobs_cli.go` call `metaldogs.Enabled` (Extra) so the generate fence still installs when the plugin is on.

Tests:

- `plugins/metaldogs/settings_test.go` loads production env names `SHOPANDA_PLUGINS_METALDOGS_ENABLED` and `SHOPANDA_PLUGINS_METALDOGS_GENERATION_*` through `Host.ConfigValue`.
- Nested YAML `generation:` with provider / max_retries / max_attempts_per_design / cooldown_seconds yields the same Settings. A non-integer in that map fails Init.
- Bad ints/bools fail. Defaults stay fake / 3 / 3 / 30. Cooldown `0` stays 0. Counts `<=0` stay 3.
- `cmd/api/register_plugins_metaldogs_test.go` checks registration, GET/HEAD `no-store` for `/api/customizer/enabled` and `/api/customizer/designs/design-1`, and `SHOPANDA_PLUGINS_METALDOGS_ENABLED=maybe` fails Init.

Env names (working):

- `Config.Extra` is `map[string]map[string]string`. `SHOPANDA_PLUGINS_METALDOGS_GENERATION_PROVIDER` becomes key `generation_provider`, not `generation.provider`. The plugin accepts that key and the dotted alias `generation.provider`.

## Shopanda gaps

- `pathPrefixFromPattern` (`internal/platform/plugin/public_routes.go`) should cut the path at the first `{`. Until that ships, the dummy `GET /api/customizer` route stays. Remove it once the prefix is `/api/customizer` without that route.
- `yamlMapToStringMap` (`internal/platform/config/plugins_extra.go`) stringifies a nested map (`map[...]`) instead of flattening it. Product code expands only `plugins.metaldogs.generation` into Extra keys `generation.provider`, `generation.max_retries`, `generation.max_attempts_per_design`, and `generation.cooldown_seconds`. Env `SHOPANDA_PLUGINS_METALDOGS_GENERATION_*` still arrives as `generation_*`. Drop `expandMetalDogsGeneration` when upstream flattens every Extra slug.

## Ticket 01 config criterion

Nested YAML `generation:` loads the same explicit fields as the old typed `MetalDogsGenerationConfig`. No PO decision rejects that map. Omitted keys inside the map use the documented defaults (fake / 3 / 3 / 30). The old struct zeroed an omitted `cooldown_seconds` to 0 when `plugins:` was present, because `UnmarshalYAML` replaced defaults; that wipe is not restored. A non-scalar under `generation:` fails Load. A non-integer scalar fails Init.
