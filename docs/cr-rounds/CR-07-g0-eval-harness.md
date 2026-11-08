# CR-07: G0 offline evaluation harness

- Closed: 2026-11-08
- Slices: `G0`
- Milestone progress: M1 → 1/3 slices (`G0`; `G1`/`G2` open)

## What shipped

- `plugins/metaldogs/generation`: `ImageProvider` port, `GenerationRequest` / `GenerationResult`, `ProviderError` classes, `Lookup` (`fake` only)
- `fake` adapter: deterministic PNG from prompt/photo/seed; rejects empty prompt/photo
- `generation/eval`: load JSON/YAML template stub, run photos dir → PNGs + `report.json` (validation status `skipped` until G3)
- CLI `metaldogs:eval` (`RequiresDB: false`); registered on plugin Init when enabled

## Verification results

| Item | Result |
| --- | --- |
| Fake determinism / reject | `generation` tests |
| Template + report + run | `generation/eval` tests |
| CLI register + fake run | `TestCLI_Eval_FakeProvider` |
| ImportBoundary / existing metaldogs | Pass |

## Flags / migrations / rollback

- No new flags/migrations. Command only appears when `plugins.metaldogs.enabled` and Init succeeds (media still required for Init).
- Rollback: revert CR; no HTTP surface.

## Documentation updated

- [x] ROADMAP G0 DONE; ADR-0008 harness note; plugin README; local-dev; CR-07 + log

## Follow-ups

- G1: real provider adapter + spike report against these criteria
- G2: replace file stub with versioned Template format
- G3: fill `validation` in report (stop `skipped`)
