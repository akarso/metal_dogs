# CR-23: Shopanda pin `ad14bd3a` (UPG5)

- Opened: 2026-11-20
- Pin: `ad14bd3aa9aee24886a50cb1da1ce6f3af4cf844` (`upstream/main`, shopanda#640, PR-1591)
- Previous pin: `fd4dd577…` (shopanda#629)

`fd4dd577` is the promote merge of `dev` (`abc82f51`) into `main` (`619bb4b7`, shopanda#628). Its tree equals `619bb4b7`. PR-1582…1591 are first-parent children of `619bb4b7`, not of `fd4dd577`. Merge-base of this branch and the new pin is `619bb4b7`.

## Ticket 00 — pin `ad14bd3a`

- Date: 2026-11-20
- Merge: `763fed8a`

### Conflict resolution

| File | Kept |
| --- | --- |
| `AGENTS.md` | Metal Dogs. Upstream build section is `make check` / `scripts/ci/unit.sh`; our command block already lists `go build`, ImportBoundary, and the Metal Dogs suites. No command line copied. |
| `docs/guides/DEVELOPER.md`, `docs/guides/EXTENSION_POINTS.md` | Deleted (modify/delete). Stub `docs/guides/README.md` stays |
| `docs/phase-17-product-packaging/ROADMAP.md` | Deleted (modify/delete) |
| `docs/phase-17-product-packaging/prs/PR-1582.md` … `PR-1591.md` | Deleted (new upstream PR notes) |

No conflict under `internal/`, `pkg/`, or `cmd/api`. Those paths match the pin except the allowlisted product files `cmd/api/register_products.go` and `cmd/api/register_plugins_metaldogs_test.go`.

`check-upstream-deviations.sh` allowlist is unchanged (17 paths).

### Plugin CLI

Plugin-level `Need(CapCatalogSeed)` stays. `metaldogs:catalog-seed` does not set `CLICommand.Needs` in this ticket. `metaldogs:eval` stays `RequiresDB: false`.

`cmd/api/cli_plugins.go` (pin, not a product edit): a command that `Needs` `CapCatalogSeed` must set `RequiresDB`. Empty `Needs` plus plugin-level `CapCatalogSeed` still binds the seed port on every `RequiresDB` command of that plugin. `RequiresDB: false` is not rejected for that Need.

`TestRunPluginCLICommand_TemplatesUsesBoundDB` no longer expects `metaldogs:eval` to fail with `RequiresDB is false`. It runs the command with no flags: the error is `--template is required`, and `OpenDB` is not called. A successful fake-provider run is the next ticket.

### Upstream range `fd4dd577..ad14bd3a` touching files we use

Tree diff (the promote commit is not an ancestor; content base is `619bb4b7`):

- `cmd/api/cli_plugins.go`, `pkg/pluginsdk/cli.go`, `internal/platform/cli/command.go`: `CLICommand.Needs` (only `CapCatalogSeed`). Non-DB siblings of a seed plugin run. Empty Needs on another `RequiresDB` command still over-binds.
- `cmd/api/plugin_boot.go`: same Needs check on the boot path we call (`bootServePlugins` / `bootWorkerPlugins`).
- `pkg/pluginsdk/migrator.go`: `MigrationSpec` is `= plugin.MigrationSpec`. Not adopted; Metal Dogs still uses `plugin.Migrator`.
- `PLUGINS.md`: CLI row and the Host/Adapt symbol map. Product sections kept.

Present on the pin, not called from Metal Dogs (MIG3):

| API | Commit |
| --- | --- |
| `pluginsdk.RequestIDFromContext`, exported `InjectRequestID` | `aa1b4778`, `987e621c` |
| `extapi.ParseMinorUnits`, `extapi.IsValidCurrency` | `6f2252c1`, ASCII harden `8e98c0a8` |
| `CapJobs` / `Host.Jobs` | `1aba998f`, bind-before-assert `8e98c0a8` |
| `CapMedia` / `Host.Media` | `b5a297ac`, bind-before-assert `8e98c0a8` |
| `harness.OpenIsolated` | `124239c3` |

No change in this range to cart, checkout, extension fields, shipment updater, or sync-job registration.
