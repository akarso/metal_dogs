# CR-10: G3 output validation

- Status: **open** — PR [#22](https://github.com/akarso/metal_dogs/pull/22) (not closed until merge)
- Slices: `G3`
- Milestone progress: M2 → G3 (pending merge)

## What shipped

- Package `plugins/metaldogs/generation/validate`: `Validate(png, OutputConfig) Result` with hard-fail codes `decode_failed`, `dimensions_mismatch`, `not_monochrome`, `blank_or_low_detail`; `no_transparency` in `Warnings` only until ADR-0008 Accept
- `png.DecodeConfig` size cap (`domain.MaxCanvasDimension`) before full decode; mono uses un-premultiplied RGB; blank ignores near-transparent pixels
- Shared `HasUsefulAlpha` (same decode bound) used by OpenAI adapter `HasAlpha` metadata
- Eval: `TemplateDef.OutputConfig` from v1 YAML; legacy stubs reject partial canvas, default `Monochrome: true` when omitted; hard fail → `error_class=validation_failed`; generate never ran → `validation.status=skipped`
- Fake provider renders monochrome spatial fixtures so offline eval passes G3 checks
- OCR unchanged: `NameHeuristic` only; no `name_ocr_mismatch` emission
- No job/worker wiring (C4)

## Verification results

| Item | Result |
| --- | --- |
| Fixture unit tests per code (incl. oversize IHDR, low-alpha chroma, mono ±1, silhouette) | Pass |
| Eval fake + legacy stub / v1 YAML | Pass (`validation.status=pass`) |
| OpenAI alpha helper shared | Pass |
| Docs links | Pass |

## Tests added

- `generation/validate/*_test.go` — decode/empty/oversize IHDR, dimensions, mono (±tolerance, premultiplied low-A), transparency warning with empty `Codes`, blank, transparent silhouette
- `generation/eval` — `validation.status=pass` / `skipped` / `fail`+`validation_failed`; partial `output_config` rejected; monochrome default

## Flags / migrations / rollback

- No flags, migrations, or HTTP surface
- Rollback: revert CR; eval falls back to skipping validation only if code is removed

## Documentation updated

- [x] ROADMAP G3 DONE (scoped); generation-pipeline validation table; generation-quality; ADR-0008 eval note; plugin README; CR-10; CR log

## Follow-ups

- C4: call `validate.Validate` in generate job after provider; fail closed on oversize decode; optionally share one decode with alpha metadata
- ADR-0008 Accept: possibly promote `no_transparency` to error / background-removal stage
- Byte-reading OCR → `name_ocr_mismatch` warning
