# CR-10: G3 output validation

- Closed: 2026-11-09
- Slices: `G3`
- Milestone progress: M2 → G3 done (C1/C4 still open)

## What shipped

- Package `plugins/metaldogs/generation/validate`: `Validate(png, OutputConfig) Result` with hard-fail codes `decode_failed`, `dimensions_mismatch`, `not_monochrome`, `blank_or_low_detail`; `no_transparency` recorded as warning until ADR-0008 Accept
- Shared `HasUsefulAlpha` used by OpenAI adapter `HasAlpha` metadata
- Eval: `TemplateDef.OutputConfig` from v1 YAML; legacy stubs default canvas from Parameters (or 64×64) + `Monochrome: true`; after generate, validation fills `report.json` (never `skipped` on success path); hard fail → sample `error_class=rejected_input`
- Fake provider renders monochrome spatial fixtures so offline eval passes G3 checks
- OCR unchanged: `NameHeuristic` only; no `name_ocr_mismatch` emission
- No job/worker wiring (C4)

## Verification results

| Item | Result |
| --- | --- |
| Fixture unit tests per code | Pass |
| Eval fake + legacy stub / v1 YAML | Pass (`validation.status=pass`) |
| OpenAI alpha helper shared | Pass |
| Docs links | Pass |

## Tests added

- `generation/validate/*_test.go` — decode, dimensions, mono (±tolerance), transparency warning, blank, alpha/tRNS
- `generation/eval` — expect `validation.status=pass` on happy path
- Fake still deterministic (seed / ProviderReqID)

## Flags / migrations / rollback

- No flags, migrations, or HTTP surface
- Rollback: revert CR; eval falls back to skipping validation only if code is removed

## Documentation updated

- [x] ROADMAP G3 DONE; generation-pipeline validation table; generation-quality; ADR-0008 eval note; plugin README; CR-10; CR log

## Follow-ups

- C4: call `validate.Validate` in generate job after provider
- ADR-0008 Accept: possibly promote `no_transparency` to error / background-removal stage
- Byte-reading OCR → `name_ocr_mismatch` warning
