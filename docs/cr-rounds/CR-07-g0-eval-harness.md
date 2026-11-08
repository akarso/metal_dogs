# CR-07: G0 offline evaluation harness

- Closed: 2026-11-08
- Slices: `G0`
- Milestone progress: M1 → 1/3 slices (`G0`; `G1`/`G2` open)

## What shipped

- `plugins/metaldogs/generation`: `ImageProvider` port, `GenerationRequest` (`PhotoBytes`), `GenerationResult`, `ProviderError` classes (+ `canceled`), `Lookup` (`fake` only)
- `fake` adapter: deterministic PNG from length-prefixed digest (full `int64` seed); rejects empty photo bytes / empty prompt / oversize canvas (`MaxDimension`); `ProviderReqID` = `fake-<sha256 hex>`
- `generation/eval`: JSON/YAML template stub; outputs `photo.ext.png` (no stem collisions); refuses non-empty `--out`; `report.json` includes root `parameters` + `model`; validation `skipped` until G3
- CLI `metaldogs:eval` (`RequiresDB: false`); without media Init is eval-only (CLI yes, no customizer HTTP / AssetStore)

## Verification results

| Item | Result |
| --- | --- |
| Fake determinism / seed 1 vs 257 / empty photo / cancel / oversize | `generation` tests |
| Distinct output names / non-empty out / report parameters / cancel class | `generation/eval` tests |
| CLI register without media + no HTTP routes; with media full surface | plugin tests |
| CLI fake run + non-zero on sample fail | plugin tests |
| ImportBoundary / existing metaldogs | Pass |

## Flags / migrations / rollback

- No new flags/migrations. Command appears when plugin enabled; media optional for Init (eval-only).
- Rollback: revert CR; no HTTP surface.

## Documentation updated

- [x] ROADMAP G0 DONE; ADR-0008 harness note; plugin README; local-dev; CR-07 + log

## Follow-ups

- G1: real provider adapter + spike report against these criteria
- G2: replace file stub with versioned Template format
- G3: fill `validation` in report (stop `skipped`)
