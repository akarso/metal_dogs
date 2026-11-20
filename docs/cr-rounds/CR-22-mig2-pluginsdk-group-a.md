# CR-22: MIG2 pluginsdk group A (simple replacements)

- Status: closed 2026-11-20 (group A). Items 4 and `metaldogs:eval` on the real CLI path wait for MIG3.
- Slices: MIG2 (behaviour unchanged)

## What changed

| # | Was | Now | Verified |
| --- | --- | --- | --- |
| 1 | `plugin.go` imported `internal/platform/logger` | `pluginsdk.Logger` (same Info/Warn/Error). `app.Logger` still satisfies it | `go test ./plugins/metaldogs/ ./cmd/api/` |
| 2 | `sync_generate.go` `apperror.Is(err, CodeConflict)` | `pluginsdk.IsConflict` | `TestEnqueueGenerate_WrappedQueueConflictUsesConflictPath` |
| 3 | `sync_prepare_production.go` same `apperror.Is` | `pluginsdk.IsConflict` | `TestEnqueuePrepare_WrappedQueueConflictUsesConflictPath` |
| 9 | `catalog/seed.go` `domain/catalog` `StatusDraft` / `TypeConfigurable` | literals `"draft"` and `"configurable"` | E1 seed tests in the gate; CSV still requires those values |
| 10 | `plugin.go` `rbac.JobsRead` | plugin constant `permJobsRead = "jobs.read"` via `Host.RegisterAdminRoute` | literal matches `internal/domain/rbac/permission.go` `JobsRead Permission = "jobs.read"`; `TestRegisterPlugins_MetalDogs_Enabled_RouteAndMigrator` still expects `rbac.JobsRead` |
| 11 | `ports/ids.go` `internal/platform/id` | `github.com/google/uuid` `NewString` | core `id.New` is UUID v4, lowercase `8-4-4-4-12`, version nibble 4, variant `10xx` (`internal/platform/id/id.go`). `TestUUIDGenerator_CanonicalV4` pins that shape |
| 12 | `settings.go` `Enabled(*config.Config)` | `Enabled(map[string]string)` from `cfg.Plugins.Extra["metaldogs"]` in `cmd/api/register_products.go`. Generation settings still use `Host.ConfigValue` | `TestEnabled_ExtraMap` (`nil`, `""`, `false`, `true`, `maybe`); existing register tests for empty / `maybe` / `true` / `false` |

`pluginsdk.IsConflict` is `apperror.Is(CodeConflict)` or `domain/extension.ErrStringValueConflict`. The job queue returns `apperror.Conflict` with no wrap (`internal/infrastructure/postgres/job_queue.go`). The new tests wrap that same error with `fmt.Errorf("%w")`. A wrapped non-conflict does not enter requeue. `ErrStringValueConflict` is not what `Enqueue` returns.

## Ticket 02

| # | Decision | Why |
| --- | --- | --- |
| 4 | Left on `plugin.Migrator` | `Registry.CollectMigrations` / `migratorRegistration` type-assert `plugin.Migrator` (`Migration() plugin.MigrationSpec`). `pluginsdk.Migrator` returns `pluginsdk.MigrationSpec`. Those are different named types, so a `plugin.App` plugin that implements only the SDK interface is skipped. `pluginsdk.Adapt` forwards it, and Adapt requires `pluginsdk.Plugin`. That move is MIG3. |
| 5 | `pluginsdk.NewHost(...).CLI().RegisterCommand` | Same registry, `RequiresDB` and `cmd.Plugin` still set from `CurrentPluginName`. |
| 6, 7 | `Host.BootstrapDB` after `Require(CapDB)` during Init | `pluginsdk.CLIContext` has no `*sql.DB` and no config. The CLI root opens the pool after `InitAll` (`FreezePluginNeeds`). CapDB has to be recorded while Init still runs, including when `Bootstrap.DB` is nil. `metaldogs:templates` is `RequiresDB: true` so load/publish/retire see that pool. `validate` does not query. The shared pool is not closed. |
| 8 | `Host.RegisterField(extapi.FieldDef)` | Same strings as the domain constants (`string`, `cart_item`, `snapshot`, `public`, `PluginOnlyWrite`, UUID regex). Owner stays `metaldogs` via `AsHostFor`. |

`metaldogs:eval` stays `RequiresDB: false` (`TestPlugin_Init_Enabled_RegistersObsPing`). The real CLI path (`runPluginCLICommand`) rejects it: the plugin `Needs` `CapCatalogSeed`, and `cmd/api/cli_plugins.go` refuses any command of that plugin with `RequiresDB` false before `Run`. Eval does not read a database; setting `RequiresDB` true would open one. Direct `cmd.Run` in the plugin tests still works.

`metaldogs:templates` on that same path used to die on the same check. `RequiresDB: true` is what lets it run. `validate` then needs a database the process can open, and still does not query it.

`metaldogs:catalog-seed` is unchanged: `RequiresDB`, `Host.CatalogSeed()`, `runPluginCLICommandWithDeps`.

`Require(CapDB)` during Init does not add a boot failure. Every production root that calls `assertServeNeedsMet`, `assertWorkerNeedsMet`, or `assertPluginCLINeedsMet` already holds a non-nil `*sql.DB` before `bindProductPorts`, and that function marks `CapDB` when `boot.DB != nil`:

- `runServe` → `wireServeRuntime` (`db.Open`, then `Bootstrap.DB`)
- `runWorker`, `runScheduler`, `runScheduleTrigger`, `newJobsRuntime`, `newCacheAdminService`, `newSearchReindexService`, `defaultOpenExtensionFieldRetireRuntime` (`db.Open`, then `newPluginBootstrap`, which rejects a nil pool)
- `syncDiscoveryFacetsFromDB` (`newPluginBootstrap` on the caller's pool; a nil pool returns before the assert)
- plugin CLI `bindPluginCLIPorts` only after `OpenDB`, and only when `RequiresDB` is true

`bootServePlugins`, `bootWorkerPlugins`, and `plugin_ports_test.go` can assert with a nil boot. Those tests do not register metaldogs. `metalDogsBootFixture` has no pool, so it marks `CapDB` itself. That keeps the catalog-seed skip assertion. It is not a production root.

## Baseline lines gone

`config/tests/importboundary-baseline.txt` is not edited here. These lines no longer appear (ticket 01 plus ticket 02):

- `metaldogs/plugin.go imports disallowed github.com/akarso/shopanda/internal/platform/logger (not on allowlist)`
- `metaldogs/plugin.go imports disallowed github.com/akarso/shopanda/internal/domain/rbac (not on allowlist)`
- `metaldogs/sync_generate.go imports disallowed github.com/akarso/shopanda/internal/platform/apperror (not on allowlist)`
- `metaldogs/sync_prepare_production.go imports disallowed github.com/akarso/shopanda/internal/platform/apperror (not on allowlist)`
- `metaldogs/catalog/seed.go imports disallowed github.com/akarso/shopanda/internal/domain/catalog (not on allowlist)`
- `metaldogs/ports/ids.go imports disallowed github.com/akarso/shopanda/internal/platform/id (not on allowlist)`
- `metaldogs/settings.go imports disallowed github.com/akarso/shopanda/internal/platform/config (not on allowlist)`
- `metaldogs/cli.go imports disallowed github.com/akarso/shopanda/internal/platform/cli (not on allowlist)`
- `metaldogs/cli.go imports disallowed github.com/akarso/shopanda/internal/platform/config (not on allowlist)`
- `metaldogs/cli.go imports disallowed github.com/akarso/shopanda/internal/platform/db (not on allowlist)`
- `metaldogs/cli.go imports disallowed github.com/akarso/shopanda/internal/platform/plugin (not on allowlist)`
- `metaldogs/design_field.go imports disallowed github.com/akarso/shopanda/internal/domain/extension (not on allowlist)`
- `metaldogs/design_field.go imports disallowed github.com/akarso/shopanda/internal/platform/plugin (not on allowlist)`

`catalog/seed.go` still imports `internal/domain/shared` (currency). That line stays. `migrations.go` still imports `internal/platform/plugin`.

`go test ./plugins/ -run ImportBoundary` now prints 17 violations (baseline file still has 30). No new line.

## MIG3 (17 import lines that stay)

- `platform/plugin` (`plugin.App` and, for migrations, `plugin.Migrator`): `assets.go`, `cart_validate.go`, `httpapi/health.go`, `migrations.go`, `plugin.go`, `sync_generate.go`, `sync_obs.go`, `sync_prepare_production.go`
- `domain/jobs`: `application/generate_worker.go`, `httpapi/health.go`, `sync_generate.go`, `sync_prepare_production.go`
- `domain/media`: `adapters/mediastore/store.go`, `assets.go`
- `platform/requestctx`: `observability/correlation.go`
- `domain/shared`: `catalog/seed.go`
- `platform/migrate`: `testutil/db.go`

`metaldogs:eval` on `runPluginCLICommand` is the same follow-up (CapCatalogSeed + `RequiresDB: false`). It is not an import line.

## Not in this change

No Shopanda change and no pin change. The plugin stays a `plugin.App` plugin.

## Flags / rollback

No new flag and no migration. Rollback is reverting the commit. `plugins.metaldogs.enabled` parsing is unchanged: missing / `0` / `false` / `no` stay off; `1` / `true` / `yes` stay on; anything else is an error and the plugin is still registered so Init fails.
