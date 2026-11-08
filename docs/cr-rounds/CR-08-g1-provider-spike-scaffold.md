# CR-08: G1 provider spike scaffold (OpenAI)

- Closed: 2026-11-08 (scaffold)
- Slices: `G1` (scaffold only — **exit gate open**)
- Milestone progress: M1 → G0 done; G1 paid spike pending; G2 open

## What shipped

- `generation/openai`: Images `edits` adapter, error class mapping, no raw payload logging
- Contract tests (httptest) + opt-in smoke (`METALDOGS_SMOKE=1`)
- `metaldogs:eval -provider openai` (+ `-openai-api-key` / `METALDOGS_OPENAI_API_KEY`)
- Five spike templates + scorecard example + OCR name heuristic stub
- `docs/architecture/generation-quality.md`; ADR-0008 candidate fields filled, status still Proposed

## Verification results

| Item | Result |
| --- | --- |
| OpenAI contract happy / 429 / cancel | Pass (local) |
| OCR heuristic | Pass |
| Existing metaldogs / eval / fake | Pass |
| Paid smoke / quality exit gate | **Not run in this CR** |

## Flags / migrations / rollback

- Env: `METALDOGS_OPENAI_API_KEY`, `METALDOGS_SMOKE` (smoke only)
- No DB migrations; no production HTTP generation route
- Rollback: revert CR; fake remains default

## Documentation updated

- [x] ROADMAP G1 scaffold / exit gate open; ADR-0008 candidate; generation-quality; CR-08; plugin README

## Follow-ups

- Operator: run spike (≥20 photos × 5 templates), fill scorecards, accept ADR-0008 if ≥4/5 pass
- Confirm commercial-use / retention terms in ADR from OpenAI docs at accept time
- G2 template format replaces spike JSON stubs
