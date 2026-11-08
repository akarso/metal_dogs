# Generation quality (G1 spike)

Status: **scaffold ready; paid exit gate open**. No customer photos in this repo.

## Candidate provider

| Item | Value |
| --- | --- |
| Adapter | `plugins/metaldogs/generation/openai` |
| API | OpenAI Images `POST /v1/images/edits` (`image[]`, no `response_format`, `background=transparent`) |
| Model (default) | `gpt-image-2.5-sunburst` (`gpt-image-1` invalid; `gpt-image-1-mini` deprecated but allowed) |
| Size | Unset → `1024x1024`; explicit WxH must be ÷16, aspect 1:3–3:1, edges ≥256 ≤2048 |
| Auth | `METALDOGS_OPENAI_API_KEY` (never logged) |
| Cost fields | `usage_*_tokens` in `report.json`; `cost_minor_units` left 0 until pricing table pinned |
| Contract tests | httptest fixtures (no network; assert MIME + no `response_format`) |
| Smoke | `METALDOGS_SMOKE=1` + API key (256² PNG fixture) |

ADR-0008 stays **Proposed** until the operator spike meets the exit gate (≥4 of 5 templates).

## How to run the spike

1. Collect ≥20 local photos (varied quality, breeds, lighting). Do not commit them.
2. For each of the five templates under `plugins/metaldogs/generation/eval/testdata/spike/`:

```bash
export METALDOGS_OPENAI_API_KEY=sk-...
./app metaldogs:eval \
  -template plugins/metaldogs/generation/eval/testdata/spike/01-head-front-blackmetal.json \
  -photos /path/to/spike-photos \
  -out /tmp/md-spike-01 \
  -provider openai
# optional: -openai-model … -openai-base https://api.openai.com/v1
```

Confirm one live smoke before the full ≥20×5 matrix (field name / MIME / model). Then:

3. Copy `scorecard.example.json` → `scorecard.json` beside `report.json` and rate each sample.
4. Summarize medians / name accuracy / cost into this file (no photos). Then move ADR-0008 → Accepted.

## Rubric (from ADR-0008)

| Criterion | Pass rule |
| --- | --- |
| Dog identity | median ≥ 4 / 5 |
| Pattern fidelity | median ≥ 4 / 5 |
| Name spelling + legibility | ≥ 90 % of samples |
| Monochrome | ≥ 95 % |
| Alpha / clean background | ≥ 90 % |
| Cost per accepted artwork | under operator budget (incl. failures) |

## Name check

`generation/ocr.NameHeuristic` only records the expected name for human review (`manual_required`). Real OCR is deferred to G3.

## Open questions answered by this spike

Project description §10 items **1, 2, 3, 7** (fidelity, cost, print usability, Polish typography).
