# CR-08: G1 provider spike scaffold (OpenAI)

- Closed: 2026-11-08 (scaffold)
- Slices: `G1` (scaffold only — **exit gate open**)
- Milestone progress: M1 → G0 done; G1 paid spike pending; G2 open

## What shipped

- `generation/openai`: Images `edits` adapter aligned to live GPT image API (`image[]`, `image/*` MIME, no `response_format`, reject shut-down `gpt-image-1`, default `gpt-image-2.5-sunburst`)
- Template `model` + `parameters` WxH drive the call; `background=transparent` + `output_format=png`; `HasAlpha` from decode; `usage_*_tokens` in result/`report.json`
- Contract tests (httptest: MIME, no `response_format`, 400/empty/`image[]`) + opt-in smoke (`METALDOGS_SMOKE=1`, 256² fixture)
- `metaldogs:eval -provider openai` (+ `-openai-api-key` / `-openai-model` / `-openai-base` / `METALDOGS_OPENAI_API_KEY`)
- Five spike templates + scorecard example + OCR name heuristic stub
- `docs/architecture/generation-quality.md`; ADR-0008 candidate fields filled, status still Proposed

## Verification results

| Item | Result |
| --- | --- |
| OpenAI contract happy / MIME / no `response_format` / 400 / 429 / empty / cancel | Pass (local) |
| OCR heuristic | Pass |
| Existing metaldogs / eval / fake | Pass |
| Paid smoke / quality exit gate | **Not run in this CR** |

## Flags / migrations / rollback

- Env: `METALDOGS_OPENAI_API_KEY`, `METALDOGS_SMOKE` (smoke only)
- CLI: `-openai-model`, `-openai-base`
- No DB migrations; no production HTTP generation route
- Rollback: revert CR; fake remains default

## Documentation updated

- [x] ROADMAP G1 scaffold / exit gate open; ADR-0008 candidate; generation-quality; CR-08; plugin README

## Follow-ups

- Operator: one live smoke, then spike (≥20 photos × 5 templates), fill scorecards, accept ADR-0008 if ≥4/5 pass
- Confirm commercial-use / retention terms in ADR from OpenAI docs at accept time
- Map `usage_*_tokens` → `cost_minor_units` when pricing is pinned
- G2 template format replaces spike JSON stubs
- Optional: 429 `Retry-After` backoff for large matrices
