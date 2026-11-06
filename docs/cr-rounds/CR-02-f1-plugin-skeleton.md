# CR-02: F1 plugin skeleton

- Closed: 2026-11-06
- Slices: `F1`
- Milestone progress: M0 → 2/6 slices (`F0`, `F1` done)

## What shipped

`plugins/metaldogs` registers behind `plugins.metaldogs.enabled` / `SHOPANDA_PLUGINS_METALDOGS_ENABLED` (default `false`). When enabled: `GET /api/customizer/health` → `200` JSON; when disabled / not registered → `404`. Implements `plugin.Migrator` with `metaldogs_meta` placeholder table. Hexagonal package stubs: `domain/`, `application/`, `ports/`, `adapters/`, `httpapi/`. ADR-0003 verification filled (sync job naming + Migrator).

## Verification results

| Item | Result |
| --- | --- |
| Flag default off | Config unit test + Init disabled error |
| Health 404 / 200 | `TestHealth_FlagOff_404`, `TestHealth_FlagOn_200` |
| ImportBoundary | Passes with new plugin |
| Migrator | `Migration()` embed FS; ADR-0003 notes |
| Free-form `metaldogs.*` jobs | Confirmed unsupported; use `integration.sync.metaldogs.*` |

## Tests added

- `plugins/metaldogs/plugin_test.go`
- `internal/platform/config` default `MetalDogs.Enabled=false`

## Flags / migrations / rollback

- Flag default off — no production behaviour change until enabled.
- Migration `001_create_meta.sql` applies when the plugin is registered (enabled) and `app migrate` / boot runs; expand-only. Rollback: disable flag; table may remain (harmless).

## Documentation updated

- [x] ROADMAP F1 DONE; ADR-0003 Verification; overview deviations; plugin README; AGENTS / local-dev enable notes
- [x] This CR record + log link

## Follow-ups filed

- `F2`: PostgreSQL test harness + CI feature tests
- `C4`: register `integration.sync.metaldogs.generate` + accept ADR-0003
- GitHub Actions billing lock blocked CI on F0 push — resolve account billing to get real Actions green
