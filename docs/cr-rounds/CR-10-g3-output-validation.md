# CR-10: G3 output validation

- Status: **open** — PR [#22](https://github.com/akarso/metal_dogs/pull/22) (not closed until merge)
- Slices: `G3`
- Milestone progress: M2 → G3 (pending merge)

## What shipped

- Package `plugins/metaldogs/generation/validate`: `Validate(png, OutputConfig) Result` with hard-fail codes `decode_failed`, `dimensions_mismatch`, `not_monochrome`, `blank_or_low_detail`; `no_transparency` in `Warnings` only until ADR-0008 Accept
- `png.DecodeConfig` size cap (`domain.MaxCanvasDimension`) before full decode; mono uses un-premultiplied **16-bit** RGB; blank ignores near-transparent pixels
- Shared `HasUsefulAlpha` (same decode bound) used by OpenAI adapter and Fake `HasAlpha` (final PNG bytes after border)
- Eval: `TemplateDef.OutputConfig` from v1 YAML; legacy stubs reject partial/non-positive canvas and **parameters≠canvas**; omitted parameters inherit canvas; hard fail → `error_class=validation_failed`; `validation.status=skipped` with distinct messages (generate did not run vs output not persisted)
- Domain: model WxH must match canvas when set; omitted model size → `GenerationWidthHeight` returns canvas; file YAML omitted `monochrome` defaults `true`
- `Validate` fail-closed on unset/partial canvas; mono vs blank alpha floors documented for C4 decision
- Fake provider renders monochrome spatial fixtures so offline eval passes G3 checks (≥4×4)
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

- `generation/validate/*_test.go` — decode/empty/oversize IHDR, dimensions, mono (±tolerance, premultiplied low-A, low-A Δ=17 edge), transparency warning with empty `Codes`, blank, transparent silhouette
- `generation/eval` — `validation.status=pass` / `skipped` / `fail`+`validation_failed`; partial/`params≠canvas` rejected; canvas-only syncs parameters; monochrome default
- `domain` — model/canvas mismatch rejected; omitted model size → canvas; file omit mono → true
- `generation` — Fake `HasAlpha` matches `HasUsefulAlpha` on final bytes (incl. ≤5×5 border clobber)

## Flags / migrations / rollback

- No flags, migrations, or HTTP surface
- Rollback: revert CR; eval falls back to skipping validation only if code is removed

## Documentation updated

- [x] ROADMAP G3 DONE (scoped); generation-pipeline validation table; generation-quality; ADR-0008 eval note; plugin README; CR-10; CR log

## Follow-ups

- C4: call `validate.Validate` in generate job after provider; fail closed on oversize decode; optionally share one decode with alpha metadata
- ADR-0008 Accept: possibly promote `no_transparency` to error / background-removal stage
- Byte-reading OCR → `name_ocr_mismatch` warning
