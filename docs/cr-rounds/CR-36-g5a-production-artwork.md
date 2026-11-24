# CR-36: G5a production artwork pipeline

- Status: pipeline in the tree; **G5 stays open** until P0 records real printer values
- Closed: 2026-11-23 (pipeline only)
- Slices: `G5a` (G5 partial)
- Milestone progress: M3 → G5 partial

## What shipped

The prepare job produces a print file when `plugins.metaldogs.production_spec` is complete. No new flag. No hard-coded printer sizes.

- Settings: `width_px`, `height_px`, `dpi`, `require_transparency`, `color_profile` (`srgb` only), `upscale_policy` (`reject|upscale`). No defaults. Section absent → spec absent. Any missing or invalid field ends that plugin's Init: worker, scheduler, and the jobs CLI refuse to start; `serve` starts with metaldogs inactive (no routes and no jobs).
- `production.Prepare` fits an approved PNG: aspect within 1% or `aspect_mismatch` (no crop, no pad); larger is Lanczos downscale to the spec size; smaller is `too_small` or Lanczos upscale; transparency required but no alpha is `transparency_required`; undecodable input is `artwork_unreadable`. Output PNG carries pHYs and an sRGB marker. Checksum is `sha256:<hex>` of those bytes.
- Migration `017` adds nullable `production_failure_reason`. The append-only trigger still allows only `production_*` changes.
- Job `prepare_production_artwork` enqueues with `MaxRetries=3`. Spec absent: error `pending_spec`, version stays `pending`, that delivery is the last. Version already `ready`: done, no write. Otherwise: read artwork, Put under `ObjectKey("production", …)` with asset id = UUID v5 of the version id, then one transaction inserts the asset and sets `ready` from `pending|preparing` only. Permanent failure: version `failed` plus reason, job failed visibly. Storage or database error: retry, version stays `pending` or `preparing`.
- After `ready`, the handler calls `FillSnapshotProduction`. That error is logged and does not fail the job.
- Reclaim of failed/cancelled/stale processing is unchanged. Logs do not carry storage keys or image bytes.
- Nothing under `internal/*`, `pkg/*`, or core `cmd/api/*`.

## Decisions

- The spec is plugin configuration until P0. ADR-0009 stays Proposed for the provider and the accepted file table. `docs/architecture/production-spec.md` is still not written.
- One spec per product and one production file per `DesignVersion`. Per-size files would change the data model.
- A failed version is not refit by reclaiming the job. A new approval is a new version.

## Verification results

| Item | Result |
| --- | --- |
| `TestPrepareProductionJob` | In the tree. Isolated Postgres, in-memory store, test specs. Not skipped |
| `production.Prepare` and settings tests | In the tree |
| `bash .github/scripts/check-product-docs-links.sh` | Run with this doc pass |

## Tests added

- `plugins/metaldogs/sync_prepare_production_job_test.go` — `TestPrepareProductionJob`: ready fills snapshot (rerun does not Put again); `too_small`; upscale at spec size; `aspect_mismatch`; `transparency_required`; `artwork_unreadable`; spec absent leaves `pending` and errors `pending_spec`; storage failure then retry; fill error does not fail the job
- `plugins/metaldogs/production/prepare_test.go` — rules, aspect boundary, upscale, transparency, no crop/pad, checksum, DPI/sRGB
- `plugins/metaldogs/settings_test.go` — spec absent, complete, each missing field, invalid profile/policy, YAML and env
- `plugins/metaldogs/adapters/postgres/design_version_repo_test.go` — `production_failure_reason` may change; a frozen column written with it does not

## Flags / migrations / rollback

- No new flag. Omit `production_spec` and behaviour matches the previous `pending_spec` stub. Rollback of the pipeline is removing that section.
- Migration `017` `production_failure_reason`. The column stays.
- Example keys without printer values: `configs/config.example.product.yaml`.

## Documentation updated

- [x] `docs/ROADMAP.md`: G5 **partial** (G5a). Stays open until P0. E4a/E4b/E5 known limits and the C6 enqueue note match the configured-spec path.
- [x] `docs/architecture/flows/checkout-snapshot-and-fulfillment.md`: spec absent versus ready plus `FillSnapshotProduction`.
- [x] `docs/architecture/flows/generation-pipeline.md` and `design-to-order.md`; `docs/architecture/api.md`; `docs/architecture/domain-model.md` (`production_failure_reason`).
- [x] ADR-0006 status table: reason column. ADR-0009: config-supplied spec until P0. `docs/adr/README.md`.
- [x] `docs/runbooks/generation-job-stuck-or-failed.md`: reason codes and operator action. `docs/runbooks/README.md` lists `production_spec`.
- [x] `configs/config.example.product.yaml` (keys, example-only, no printer size). `plugins/metaldogs/README.md`.
- [x] Index row in this file's parent README.

## Follow-ups

- P0: real provider and file values. Until then G5 is open and ADR-0009's fulfilment table stays Proposed.
- Per-size production files, ICC/CMYK, crop or pad, regenerating a `ready` version after a spec change, POD submit (P2), admin UI: out of this round.
