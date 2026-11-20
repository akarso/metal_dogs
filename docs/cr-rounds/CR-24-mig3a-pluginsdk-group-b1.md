# CR-24: MIG3a pluginsdk group B1

- Status: tickets 01 and 02 recorded 2026-11-20. Baseline file not edited here (orchestrator removes the three lines below). Round not closed until that file matches the gate.
- Pin: `ad14bd3a` (unchanged)
- Slice: MIG3a

## Ticket 01

| Was | Now |
| --- | --- |
| `catalog.parseNetMinor` / `stripThousands` / `minorFromParts` | `extapi.ParseMinorUnits(s, 2)` plus `amountShapeE1Rejects` |
| `shared.IsValidCurrency` in `catalog/seed.go` | `extapi.IsValidCurrency` after the existing `strings.ToUpper` |
| `requestctx.RequestID` in `observability.CorrelationID` | `pluginsdk.RequestIDFromContext`. Function kept |

CSV columns and the seed error text stay `metaldogs:catalog-seed: prices.csv: row N: column amount: invalid amount %q`.

## Price: `ParseMinorUnits` vs the old parser

Exponent is 2. Compared by running the old function and `ParseMinorUnits` on every string of length 0–7 over `0123456789.,`, plus the E1 examples and the shapes below. When both accept, the minor unit matches. The old parser never accepts a string `ParseMinorUnits` rejects.

`ParseMinorUnits` accepts strings the E1 parser rejects. Those stay rejected in `amountShapeE1Rejects`. Nothing in that set was switched to accept.

| Input | Old parser | `ParseMinorUnits(..., 2)` | Decision |
| --- | --- | --- | --- |
| `80,49` `80.49` | 8049 | 8049 | same |
| `1.234,56` `1,234.56` | 123456 | 123456 | same |
| `80,4` | 8040 (one fraction digit pads) | 8040 | same |
| `80,499` `80.499` `1.234` `1,234` | reject (fraction longer than 2) | reject | same. `1,234` alone is the upstream example that becomes a decimal at exponent 3 (`TestParseMinorUnits_KWDSingleSepIsDecimal`). At exponent 2 both reject it. No extra branch |
| `0` `0,00` `-5` `+5` `""` `abc` `80 zł` `80 PLN` | reject | reject | same. Zero and a leading sign are already errors from `ParseMinorUnits`; the wrapper only rewrites the message to `invalid amount %q` |
| `1.` `80,` `1.234,` | reject (empty fraction) | accept (`1.` → 100) | keep E1 reject |
| `.50` `,49` | reject (empty integer) | accept (`.50` → 50) | keep E1 reject |
| `1.234.567` `1,234,567` | reject (thousands marks and no fraction) | accept (123456700) | keep E1 reject |
| `1234.567,89` `12345,678.90` `1234.567.890,12` | reject (leftmost thousands group longer than 3) | accept | keep E1 reject |

`TestLoadPrices_AmountStrings` pins the E1 strings and the kept rejects through `loadPrices`, including both empty sides (`80,`, `,49`), fraction-less thousands (`1,234,567`), and a thousands group wider than 3 on the dot-decimal side (`12345,678.90`). The assertion is the full `metaldogs:catalog-seed: prices.csv: row 2: column amount: invalid amount %q` text.

## Currency

`extapi.IsValidCurrency` and `shared.IsValidCurrency` are both `^[A-Z]{3}$`. The plugin rule was not stricter. `loadPrices` still uppercases the cell before the check, so `pln` stays valid. The table test writes `pln` and expects `PLN`.

## Request id

`pluginsdk.RequestIDFromContext` returns `requestctx.RequestID`. `observability/correlation_test.go` is unchanged. It still plants the id with `requestctx.WithRequestID`.

## Ticket 02

| Was | Now |
| --- | --- |
| `testutil.openIsolated` opened the pool, created `md_<slug>_<pid>_<n>`, and called `migrate.Run` / `migrate.RunPluginFS` | `harness.OpenIsolated`. Signatures of `OpenIsolated` and `OpenIsolatedWithCore` unchanged |
| `internal/platform/migrate` import in `testutil/db.go` | gone. Plugin SQL via `PluginName` + `pluginsdk.MigrationSpec` (`MigrationFS`, `MigrationDir`) |
| Core SQL from `runtime.Caller` → `migrations/` next to this module | empty `CoreMigrationsDir` (harness walks cwd for `migrations/001_create_products.sql`). `OpenIsolated` passes `"-"` and skips core |
| `db_config_test.go` (`configWithSearchPath` URL and keyword DSN) | removed with the local parser. DSN parsing lives in `pkg/pluginsdk/harness` (not edited) |

`pluginsdk.ApplyMigrationFS` calls `migrate.RunPluginFSOptions` with a zero `PluginFSOptions`, the same default as `migrate.RunPluginFS`: per-file advisory lock, key per plugin name, `DefaultPluginLockWait` when the context has no deadline. The CI `-p 1` note still holds. `migrations.go` still tells tests to go through `testutil.OpenIsolated`.

## Semantic differences (test DB)

Call sites and assertions stay. These details changed inside the helper:

| Topic | Before | Harness | Decision |
| --- | --- | --- | --- |
| Schema name | `md_<slug>_<pid>_<counter>`, truncated to 63 bytes | `h` + 16 hex digits (8 bytes from `crypto/rand`) | `packageSlug` is unused for naming. Same slug, two calls, still two schemas (`TestOpenIsolated_SameSlugDistinctSchemas`) |
| `search_path` | `pgx` `RuntimeParams` only | `RuntimeParams` plus `SET search_path` on every connect | still forced; a path in the DSN is overwritten |
| Pool size | `SetMaxOpenConns(8)` | `database/sql` default (unlimited) | callers that need a cap set it (`TestOpenIsolated_PoolSeesSchema`) |
| Drop failure | `t.Logf` | error discarded | cleanup still `DROP SCHEMA … CASCADE` after `db.Close` |
| Skip text | `SHOPANDA_TEST_DSN not set; skipping integration test` | `SHOPANDA_TEST_DSN not set` | still `t.Skip` |
| Core dir | path from this source file, independent of cwd | walk up from the test process cwd (max 16) | `go test` cwd is the package dir under the module, so the walk finds repo `migrations/` |

## Baseline lines that no longer appear

`config/tests/importboundary-baseline.txt` is not edited. These three lines no longer appear (17 → 14):

- `metaldogs/catalog/seed.go imports disallowed github.com/akarso/shopanda/internal/domain/shared (not on allowlist)`
- `metaldogs/observability/correlation.go imports disallowed github.com/akarso/shopanda/internal/platform/requestctx (not on allowlist)`
- `metaldogs/testutil/db.go imports disallowed github.com/akarso/shopanda/internal/platform/migrate (not on allowlist)`

## Not in this change

No `plugin.App` removal, no edits under `internal/*`, `pkg/*`, or core `cmd/api/*`.
