# ADR-0008: Single image-generation provider behind a port

- **Status:** Proposed — provider, model, and parameters to be decided at close of `G1`
- **Date:** 2026-10-15
- **Related slices:** `G0`, `G1`, `G3`, `C4`, `O1`, `O3`
- **Related ADRs:** [ADR-0003](0003-async-generation-pg-job-queue-polling.md)

## Context

The project requires one provider at first, chosen on quality, cost, latency, data-processing terms, and commercial-use rights, hidden behind a small Go adapter; keys stay server-side; consumer-app capabilities must not be assumed to exist in the API. Open questions 1, 2, 3, and 7 of the project description are answered by the `G1` spike.

## Decision (structure, decided now)

1. The `generation` module defines an `ImageProvider` port:
   `Generate(ctx, GenerationRequest) (GenerationResult, error)` where the request carries the normalized photo, reference pattern, prompt text with `prompt_version`, and typed parameters; the result carries image bytes, declared alpha support, provider-reported cost/latency, and a provider request id for support.
2. Exactly two adapters exist at any time: `fake` (deterministic fixtures; default everywhere) and the one real provider. Switching is a config value (`plugins.metaldogs.generation.provider`).
3. Prompts live with the template definition and are versioned (`prompt_version`); every job records provider, model, prompt version, and parameters.
4. Provider errors are classified by the adapter into `transient` / `permanent` / `rejected_input` / `rate_limited`; raw payloads never leave the adapter.
5. If the provider cannot deliver reliable transparency, a background-removal stage is added to the pipeline (recorded here when decided).
6. If typography is not reliable for Polish names across lengths, templates may declare `text_rendering: separate_layer`, and a deterministic text layer is composited server-side for those templates (recorded here when decided).

## Decision (provider, to be filled at G1)

| Item | Value |
| --- | --- |
| Provider / API | _pending_ |
| Model and version | _pending_ |
| Parameters (size, steps, guidance, seed policy) | _pending_ |
| Alpha channel supported | _pending_ |
| Cost per call; cost per accepted artwork incl. failures | _pending_ |
| p50 / p95 latency | _pending_ |
| Data processing and retention terms (photos) | _pending_ |
| Commercial use of outputs permitted | _pending_ |
| Rate limits | _pending_ |
| Quality score per spike template (≥ 4 of 5 must pass) | _pending_ |
| Name typography correctness (Polish diacritics, 3–12 chars) | _pending_ |

## Evaluation criteria for G1

- Dog identity preserved (recognisable breed, markings, face) — human rating 1–5, pass ≥ 4 median.
- Pattern fidelity to the reference template — pass ≥ 4 median.
- Dog name legible and correctly spelled — ≥ 90 % of samples.
- Monochrome output within validation tolerance — ≥ 95 %.
- Usable alpha or clean background for removal — ≥ 90 %.
- Cost per accepted artwork (including failed attempts) under the per-order budget set by the operator.

## Harness (G0)

Offline CLI `metaldogs:eval` writes PNGs + `report.json` (latency, cost slot, validation placeholder `skipped` until G3). Default provider `fake` is deterministic. Real-provider spike still G1; this ADR stays Proposed until that close.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Multiple providers with fallback from day one | Doubles contract surface and evaluation effort before quality is known |
| Self-hosted model | Out of scope per project description |
| Python sidecar for provider calls | Excluded; Go adapter suffices |

## Consequences

- Positive: provider switch is a config change plus one adapter; costs and versions are reproducible per job.
- Negative: single-provider outage stops generation. Mitigation: cost cap and outage runbook; designs persist and resume.

## Compliance

- Contract test for the real adapter against sanitized recorded fixtures (no network).
- Smoke test behind `METALDOGS_SMOKE=1`; never in default CI.
- Test asserting `GenerationJob` rows always have non-empty `provider`, `model`, `prompt_version`.
