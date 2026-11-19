# CR-22: MIG2 pluginsdk group A (simple replacements)

- Status: ticket 01 done; items 4–8 are ticket 02
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

## Baseline lines gone

`config/tests/importboundary-baseline.txt` is not edited here. These lines no longer appear:

- `metaldogs/plugin.go imports disallowed github.com/akarso/shopanda/internal/platform/logger (not on allowlist)`
- `metaldogs/plugin.go imports disallowed github.com/akarso/shopanda/internal/domain/rbac (not on allowlist)`
- `metaldogs/sync_generate.go imports disallowed github.com/akarso/shopanda/internal/platform/apperror (not on allowlist)`
- `metaldogs/sync_prepare_production.go imports disallowed github.com/akarso/shopanda/internal/platform/apperror (not on allowlist)`
- `metaldogs/catalog/seed.go imports disallowed github.com/akarso/shopanda/internal/domain/catalog (not on allowlist)`
- `metaldogs/ports/ids.go imports disallowed github.com/akarso/shopanda/internal/platform/id (not on allowlist)`
- `metaldogs/settings.go imports disallowed github.com/akarso/shopanda/internal/platform/config (not on allowlist)`

`catalog/seed.go` still imports `internal/domain/shared` (currency). That line stays.

## Not in this change

Items 4–8 (migrator, CLI, `Host.BootstrapDB`, `RegisterField`) stay on `plugin.App`. Ticket 02.

## Flags / rollback

No new flag and no migration. Rollback is reverting the commit. `plugins.metaldogs.enabled` parsing is unchanged: missing / `0` / `false` / `no` stay off; `1` / `true` / `yes` stay on; anything else is an error and the plugin is still registered so Init fails.
