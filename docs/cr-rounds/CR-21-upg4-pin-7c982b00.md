# CR-21: Shopanda pin `7c982b00` (UPG4 ticket 00)

- Status: pin merged; later UPG4 tickets still delete deviations
- Date: 2026-11-17
- Slice: UPG4 ticket 00
- Base: `3593795d` (`origin/main` after PR #40)
- Pin: `7c982b00…` (`upstream/main`, PR #606)

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

Closed on pin `ab35e965` (ticket 00b). Record of what ticket 01 was waiting on:

- `pathPrefixFromPattern` now cuts at the first `{`. `GET /api/customizer` stays: param routes denylist `/api/customizer/designs`, not `/api/customizer`, and `/api` alone is refused.
- `yamlMapToStringMap` flattens nested maps to dotted keys and stores `.` / `_` aliases. `expandMetalDogsGeneration` is removed. Lists fail Load.

Open after ticket 01b (Shopanda roadmap; not done in this tree):

- Drop typed `MetalDogsPluginConfig` and remove `metaldogs` from `typedPluginYAMLKeys`. The pin still has both. This tree already deleted the struct, so putting the key back would drop `plugins.metaldogs` YAML and `SHOPANDA_PLUGINS_METALDOGS_*` (`isTypedPluginEnvRest`) with nowhere to store them. Accepted in ticket 01c: `config.go` and `plugins_extra.go` stay on the allowlist. Reason: upstream still types metaldogs; remove when Shopanda drops it.
- `setPluginExtraKey` aliases only all-`_` ↔ all-`.`. `SHOPANDA_PLUGINS_METALDOGS_GENERATION_MAX_RETRIES` writes `generation_max_retries` and `generation.max.retries`. Nested YAML `generation.max_retries` is a third key and is not overwritten. Same for `cooldown_seconds` and `max_attempts_per_design`. `generation_provider` ↔ `generation.provider` is a full swap, so `provider` is fine. The plugin reads the underscore key first so env still wins. Upstream should overwrite the mixed key or reject it as the same alias.
- `applyPluginsExtraEnv` stores empty values. Typed plugin env skips `""`. Empty `SHOPANDA_PLUGINS_METALDOGS_ENABLED` overwrites YAML `enabled: true` and the plugin turns off. Ticket 01c documents this as a known difference (old code ignored an empty value), pending a Shopanda change to skip empty env.

## Ticket 01 config criterion

Nested YAML `generation:` loads the same explicit fields as the old typed `MetalDogsGenerationConfig`. No PO decision rejects that map. Omitted keys inside the map use the documented defaults (fake / 3 / 3 / 30). The old struct zeroed an omitted `cooldown_seconds` to 0 when `plugins:` was present, because `UnmarshalYAML` replaced defaults; that wipe is not restored. A non-integer scalar fails Init. A list under `generation:` fails Load. A further nested map flattens (the old expander rejected any non-scalar).

## Ticket 00b — pin `ab35e965`

- Date: 2026-11-18
- Pin: `ab35e965e23463126e5ad9dc85e306e13e485550` (`upstream/main`, PR #611)
- Previous pin on this branch: `7c982b00…`

### Conflict resolution

`pkg/pluginsdk/*` did not conflict (tree matched `7c982b00…`); the merge took the pin. `internal/platform/plugin/public_routes.go` likewise.

| File | Kept |
| --- | --- |
| `PLUGINS.md`, `RUNBOOK.md`, `README.md`, `configs/config.example.yaml`, `cmd/api/main.go`, `cmd/api/jobs_cli.go` | Metal Dogs. `jobs_cli_test.go` auto-merged a `parseJobsRequeueArgs` test; restored from HEAD because `jobs_cli.go` has no `jobs:requeue` |
| `cmd/api/bind_queue_test.go` and other queue test doubles | Ours, then `Requeue` gained `...jobs.RequeueOption` so they still implement `jobs.Queue` |
| `internal/domain/jobs/job.go`, `worker.go`, `worker_test.go` | Ours (`ErrSkipOutcome` text `job: skip outcome`, `DefaultRequeueMinAge` = 1m, `DefaultStaleProcessingLease`). Added pin types: `StatusReclaiming`, `RequeueOption`, `RequeueHookable`, `ErrRequeueTooFresh` / `WrongAttempt` / `NotProcessing` / `InProgress`. `Queue.Requeue` is variadic |
| `internal/infrastructure/postgres/job_queue.go` (+ test) | Ours. `Requeue` accepts and ignores options; min-age stays `DefaultRequeueMinAge`; fenced types still refused outside the re-pend transaction |
| `internal/platform/plugin/providers.go` (+ test) | Ours (`BoundQueue` fallback, no `queueChainContains`) |
| `internal/platform/plugin/queue_hooks.go` (+ test) | Ours, plus the pin hook signature `(ctx, id, attempt) error`. Hook still runs after commit; error and panic do not fail `Requeue` |
| `internal/platform/config/config.go` | Ours (no typed `MetalDogs`). `applyEnv` returns the Extra env error |
| `internal/platform/config/plugins_extra.go` | Pin flatten / alias conflict rules. `metaldogs` stays out of `typedPluginYAMLKeys` (pin lists it; our typed field is already gone, so keeping the key would drop YAML and env). `expandMetalDogsGeneration` removed |
| `internal/application/integration` sync tests, `scheduler_test.go` | Ours, variadic `Requeue` |
| `.github/workflows/ci.yml` | Auto-merge: Metal Dogs jobs kept; integration job gained upstream `redis5` + `SHOPANDA_TEST_REDIS_URL` |
| Shopanda guides `EXTENSION_POINTS.md`, `PRODUCT_SITES.md`, `docs/phase-17-product-packaging/**` | Deleted (modify/delete and new PR notes). Stub indexes stay |

### Upstream range `7c982b00..ab35e965` touching files we use

- `CatalogPriceSeed.Update` and `EnsurePrice` → `(written bool, err)` (`pkg/pluginsdk/ports.go`, `port_adapt.go`, `cmd/api/plugin_ports.go`). Create-only remains the default. Attributes stay create-only. Seed CLI is still `cmd/api/metaldogs_catalog.go` (ticket 02).
- Price tx: bridged seed `WithTx` without `*sql.Tx` fails closed (`port_adapt.go`).
- NoCache: static path before `{`; refuse empty, single-segment-after-cut, and roots `/` and `/api`. Static one-segment mounts are allowed. `GET /api/customizer` still registered at this pin bump (removed in ticket 01c).
- `Config.Extra`: nested YAML → dotted keys; `.` and `_` are aliases; conflict fails closed; lists error. Env `SHOPANDA_PLUGINS_METALDOGS_GENERATION_*` still arrives as `generation_*` and is mirrored to `generation.*`.
- `Host.OnRequeued` is `(ctx, jobID, attempt) error`. `pluginsdk.RequeueTx` reads the reclaim `*sql.Tx`. Our notify wrapper does not use it yet.
- Redis job-queue reclaim (`25158fbd`) landed in `internal/infrastructure/redis`. Metal Dogs generate fence stays on Postgres.
- `jobs:requeue --min-age` on the pin was not taken (`jobs_cli.go` stayed ours).

### Compile adaptations that are not behavior changes

- Every `jobs.Queue` stub we kept gained the variadic `Requeue` parameter.
- Postgres `Requeue` ignores `WithRequeueMinAge`. Pin default min-age is 5m; this queue still uses 1m.

## Ticket 01b — drop config and NoCache workarounds

- `expandMetalDogsGeneration` and `metalDogsExtraSlug` are already gone. The only diff versus the pin in `plugins_extra.go` is the omitted `metaldogs` entry in `typedPluginYAMLKeys`. The pin still has typed `MetalDogsPluginConfig` (`enabled` only) and lists the slug so YAML/env for it do not enter `Extra`. Ticket 01 removed that struct. Putting the key back would drop `plugins.metaldogs` YAML and `SHOPANDA_PLUGINS_METALDOGS_*` (`isTypedPluginEnvRest`) with nowhere to store them. The file stays on the allowlist. That acceptance criterion is blocked upstream (see Shopanda gaps), not done.

Alias rule in effect (`setPluginExtraKey`, YAML flatten and env overlay):

- Aliases are all-`_` ↔ all-`.` only. Both of those forms are stored. Different values on those two forms fail Load (`conflicting keys`).
- `SHOPANDA_PLUGINS_METALDOGS_GENERATION_PROVIDER` becomes `generation_provider` and `generation.provider`. Env overwrites both, so it replaces YAML.
- A multi-word key has no mixed alias. `SHOPANDA_PLUGINS_METALDOGS_GENERATION_MAX_RETRIES` becomes `generation_max_retries` and `generation.max.retries`. Nested YAML `generation: {max_retries: 2}` also stores `generation.max_retries`. Env does not write that mixed key. Same for `generation.cooldown_seconds` and `generation.max_attempts_per_design`.
- The plugin reads the underscore key first (`generation_provider`, `generation_max_retries`, `generation_max_attempts_per_design`, `generation_cooldown_seconds`), then the dotted key, through `Host.ConfigValue`. Env therefore wins over the stale mixed YAML key. Test: `TestSettingsFrom_EnvOverridesNestedYAMLMultiWordKeys`.

Empty `SHOPANDA_PLUGINS_METALDOGS_ENABLED=""`: known difference, recorded in ticket 01c. Test: `TestSettingsFrom_KnownDifference_EmptyEnabledEnvOverwritesYAML`.

Cooldown: intentional behaviour change, recorded in ticket 01c. Omitted `cooldown_seconds` inside `generation:` is 30, not 0. Test: `TestSettingsFrom_GenerationWithoutCooldownDefaultsTo30`.

NoCache: the JSON `GET /api/customizer` workaround from this ticket is removed in ticket 01c. `httpapi.NotFound` is gone.

## Ticket 01c — close the 01b leftovers

Decisions (do not re-litigate):

- Upstream `ab35e965` still has typed `MetalDogsPluginConfig` and `"metaldogs"` in `typedPluginYAMLKeys` (comment "migrate-next"). This tree does not restore the typed field and does not add Metal Dogs code to core. Putting the key back would drop `plugins.metaldogs` YAML and `SHOPANDA_PLUGINS_METALDOGS_*` (`isTypedPluginEnvRest`) with nowhere to store them. `internal/platform/config/config.go` and `plugins_extra.go` stay on the deviations allowlist. Reason on each row: upstream still types metaldogs; remove when Shopanda drops it.
- Empty `SHOPANDA_PLUGINS_METALDOGS_ENABLED=""` is a known difference pending a Shopanda change. Old code ignored an empty value. Upstream `applyPluginsExtraEnv` stores it, overwrites YAML `enabled: true`, and `ParseEnabled("")` turns the plugin off. The plugin cannot recover the YAML bool because `Host.Config*` only sees the overwritten string. Test: `TestSettingsFrom_KnownDifference_EmptyEnabledEnvOverwritesYAML`.
- Cooldown 0 → 30 is an intentional behaviour change. YAML `generation:` with `provider` / `max_retries` and no `cooldown_seconds` yields cooldown 30 (omitted counts stay 3). The old typed struct zeroed an omitted `cooldown_seconds` to 0 when the map was present, because `UnmarshalYAML` replaced defaults. That wipe is not restored. Test: `TestSettingsFrom_GenerationWithoutCooldownDefaultsTo30`.
- Bare `/api/customizer` is not an endpoint. The route is removed entirely (`plugins/metaldogs/httpapi/notfound.go` deleted). No NoCache workaround route remains. The exact path is a plain 404 and may be cached. `/api` stays refused.
- Every registered `/api/customizer/*` route uses `pluginsdk.NoCache()`. Guest GET and HEAD are `no-store` through `CacheControlMiddleware`. `GET /api/customizer/enabled` denylists `/api/customizer/enabled`. `GET /api/customizer/designs/{id}` denylists `/api/customizer/designs` (cut at `{`). Test `TestRegisterPlugins_MetalDogs_Enabled_RouteAndMigrator` lists every registered customizer route and fails if one lacks NoCache.

