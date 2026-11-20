# CR-23: Shopanda pin `ad14bd3a` (UPG5)

- Opened: 2026-11-20
- Pin: `ad14bd3a…` (`upstream/main`, shopanda#640, PR-1591)
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

`TestRunPluginCLICommand_TemplatesUsesBoundDB` covers `metaldogs:templates` only. `TestRunPluginCLICommand_EvalRunsWithoutDSN` runs `metaldogs:eval` through `runPluginCLICommandWithDeps` with `-template plugins/metaldogs/templates/head-front-blackmetal.yaml`, a one-pixel PNG under `-photos`, `-out` in a temp dir, `-provider fake`, `-dog-name X`. It requires `ran` and a nil error, `report.json` plus a PNG in `-out`, and zero `OpenDB` calls.

### Real CLI (`metaldogs:eval`, fake provider)

`go build -o app ./cmd/api` succeeded. Executing that `./app` binary was denied by the session sandbox, so the same `main` was started with `go run ./cmd/api`. No `.env`. `SHOPANDA_TEST_DSN` unset. `SHOPANDA_DATABASE_HOST=127.0.0.1` and `SHOPANDA_DATABASE_PORT=1` (a connect would fail). Also set, because `configs/config.yaml` refuses to load otherwise: `SHOPANDA_PLUGINS_METALDOGS_ENABLED=true`, `SHOPANDA_SERVER_PUBLIC_BASE_URL=http://127.0.0.1:8080`, `SHOPANDA_DEV_MODE=true`, and a 64-byte `SHOPANDA_AUTH_JWT_SECRET` (value not recorded).

```text
go run ./cmd/api metaldogs:eval \
  -template plugins/metaldogs/templates/head-front-blackmetal.yaml \
  -photos plugins/metaldogs/generation/mockup/testdata \
  -out /tmp/md-upg5-eval-burek \
  -provider fake \
  -dog-name Burek
```

Exit code 0 (2026-11-20T07:46:07Z). Stdout line: `metaldogs:eval wrote 1 ok / 0 failed → /tmp/md-upg5-eval-burek/report.json`. Log: `plugin.registered` / `plugin.init.complete` for `metaldogs`; `metaldogs.eval` with `ok=1`, `failed=0`, `provider=fake`, `template_id=head-front-blackmetal`. `core/postgres-search`, `core/postgres-cache`, and `core/postgres-queue` logged `database not configured` and did not block eval. `-out` contains `report.json` (`dog_name=Burek`, `provider=fake`, sample validation `pass`) and `black-tee.png.png`.

### UPSTREAM-CANDIDATES

The living list is `~/ai-os/state/UPSTREAM-CANDIDATES.md`, outside this worktree, so this round does not edit it. Mapping from that file onto APIs present on pin `ad14bd3a` (not adopted here except #18, which is the eval fix):

| # | Upstream API on this pin | This round |
| --- | --- | --- |
| 1 | `extapi.ParseMinorUnits` (`pkg/extapi/money_parse.go`) | available; still our `parseNetMinor` |
| 2 | `harness.OpenIsolated` (`pkg/pluginsdk/harness/isolated.go`) | available; still `plugins/metaldogs/testutil` |
| 13 | `extapi.IsValidCurrency` | available; still `domain/shared.IsValidCurrency` |
| 14 | `pluginsdk.RequestIDFromContext`, `pluginsdk.InjectRequestID` | available; still `observability/correlation.go` |
| 15 | `pluginsdk.CapJobs`, `Host.Jobs` | available; not called |
| 16 | `pluginsdk.CapMedia`, `Host.Media` | available; not called |
| 17 | `pluginsdk.MigrationSpec` = `plugin.MigrationSpec` (PR-1589) | resolved upstream; product still implements `plugin.Migrator` |
| 18 | `RequiresDB: false` sibling of a `CapCatalogSeed` plugin runs (PR-1590/1591) | resolved and used: `metaldogs:eval` runs and does not open a DB |

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
