# CR-32: E4a order design snapshot

- Status: **closed**
- Closed: 2026-11-23
- Slices: `E4a`
- Milestone progress: M3 → E4a

## What shipped

Plugin-side order snapshot. Dark. A real checkout does not write a row.

- Table `metaldogs_order_design_snapshots` (migration `015`). Primary key `(order_id, variant_id)`. Copy of the approved version: SKU, template slug and version, texts, provider, model, prompt version, artwork id and checksum, production id and checksum (nullable, set once from NULL). Deletes refused. Other columns refuse updates.
- `ports.OrderSource`: `Lines`, `ListCreatedSince`. E4a ships an in-memory fake used by tests. No adapter over `Host.Orders()`. No reads of core tables.
- Flag `plugins.metaldogs.order_snapshot.enabled`, default false. Off: `snapshot_order_design`, `dispatch_snapshot_order`, and `reconcile_snapshots` are not registered; `metaldogs:snapshots reconcile` reports and refuses `--fix`.
- On: `order.created` enqueues `dispatch_snapshot_order` (`RegisterOnEvent`). That job enqueues `snapshot_order_design` with `WithJobID` = UUID v5 of the canonical order id. One plugin transaction inserts every design line (`ON CONFLICT DO NOTHING`) and moves each design `approved → ordered` (already `ordered` is a no-op). A missing design, approved version, or pinned template fails the job, writes nothing, and leaves the Shopanda order untouched. Retries end `failed`.
- `metaldogs:snapshots reconcile` reports `order_id`, `line_key` (`order_id/variant_id`), `reason`. Default window is 30 days. `--order` inspects one id. `--fix` enqueues one snapshot job per affected order and skips `inspect_failed`. Cron `reconcile_snapshots` (`@every 5m`) logs the same report and does not enqueue. While the flag is on it calls `FillSnapshotProduction` for NULL production columns whose version is `ready`.
- `AssetsReferencedBySnapshots` is the deletion check on every plugin path that soft-deletes an asset. Nothing schedules those paths.

## Decisions

- Line key is `(order_id, variant_id)`. ADR-0005 decision 4 called it `order_line_id`. Shopanda order items have no id. The pair is the key core uses for `order_item` extension values.
- Re-order of an `ordered` design stays `409` `invalid_transition` on add-to-cart. The path is `CloneDraft`, then a new approval. `CloneDraft` from `ordered` already existed.
- E4a depends on a plugin-owned port so E4b only swaps the adapter.
- Production artwork copied when the version is `ready` at snapshot time; otherwise NULL, filled once later. G5 must call `(*metaldogs.Plugin).FillSnapshotProduction` when it sets `production_status=ready`. The prepare stub does not.

## Platform dependencies (already on the Shopanda roadmap)

These are the two known gaps. This round does not edit `internal/*`, `pkg/*`, or core `cmd/api/*`, and does not open a new platform gap.

1. `pluginsdk.OrderPort` is `FindByID` only (`pkg/pluginsdk/ports.go`). E4b needs `Lines` (extension values) and `ListCreatedSince`.
2. `CreateOrderStep` calls `publishOrderCreated` and then `SnapshotCartItemToOrderItem` (`internal/application/checkout/create_order_step.go`). `order.created` therefore runs before `metaldogs.design_id` is on the order line. Payload is `OrderCreatedData` (order id, customer id, contact email, currency, item count). E4b needs the event after that copy, or a later event, with at-least-once and idempotency documented, and a pin bump that contains both changes. Merge `main`; do not rebase.

## Known limits

- Flag off and no real order source. Tests are not a checkout.
- `OrderSource.Lines` does not distinguish a missing order from an order whose lines have no design id. Both end the job `done` with no log.
- Sweep lookback is 30 days. Older orders need `--order`.
- A protected asset on `ReplaceSource` is not a quiet skip: the unique live `(owner_ref, type)` index makes the replace transaction fail. That path is not reachable from a snapshot (see below).
- Deletion functions are unscheduled. After-`shipped` policy is O2.
- Runbook [`order-without-design-snapshot.md`](../runbooks/order-without-design-snapshot.md) is marked last exercised: never.

## Deletion paths (ticket 03)

`AssetsReferencedBySnapshots` is the only check for "does an order snapshot name this asset?" It matches `artwork_asset_id` and `production_asset_id`. A protected asset is left active (not an error) and logged once as `metaldogs.retention.asset_kept` with `path` and `asset_id`. An asset with no snapshot row is deleted as that path already deleted it.

No scheduler and no sweep were added. Nothing calls the three retention functions until a later slice does.

| Path | Entry | What it deletes | Skip |
| --- | --- | --- | --- |
| Asset cleanup | `postgres.CleanupDueAssets` | Active assets with `delete_after <= now` | Snapshot artwork and production ids |
| Unattached upload cleanup | `postgres.CleanupUnattachedUploads` | That set where `owner_ref` is null | Same |
| Design expiry | `postgres.ExpireDesigns` | Assets of designs past `expires_at` in `draft`, `generating`, `generation_failed`, `preview_ready`, `approved` (the expires index). Design becomes `expired` | Same. `ordered` designs are not selected |
| Compensating soft-delete | `AssetRepo.SoftDelete` (upload sign-failure cleanup, clone cleanup) | The named asset, then its blob only if the row is no longer active | Same |
| Replace source | `DesignRepo.ReplaceSource` → `softDeleteAsset` | Previous source asset and, when the row was marked deleted, its blob | Same. The replace transaction does not commit while the old row is still the live source |
| Replace live asset | `GenerationJobRepo.PersistArtwork`, `CompleteSuccess`, `FinalizeSuccess` → `softDeleteLiveAssetType` | Previous live asset of that owner and type, then its blob | Same |

Searched `deleted_at`, `delete_after`, and design expiry handling under `plugins/metaldogs`. Those three functions were not in the tree; this ticket added them so every deletion decision goes through one helper. Nothing schedules them. O2 calls `CleanupDueAssets`, `CleanupUnattachedUploads`, and `ExpireDesigns` and does not write a second soft-delete of the same rows. Scheduling and the post-`shipped` policy stay in O2. `delete_after` updates that only move the clock are not deletion paths.

`partitionSnapshotAssets` takes `SELECT … FOR UPDATE` on the asset rows (`ORDER BY id`) before `AssetsReferencedBySnapshots`. Every deletion path calls that helper, including `AssetRepo.SoftDelete` and `softDeleteAsset` (`ReplaceSource`). `CommitOrder` and `FillProduction` take `FOR SHARE` on the live asset rows they are about to reference, in the same transaction as the write, so a snapshot commit cannot land between the check and `deleted_at`. A missing or already soft-deleted asset fails the snapshot write and leaves the order unchanged.

## Replace of a protected asset returns an error

`ReplaceSource` does not treat a protected source as a quiet skip. `softDeleteAsset` leaves the row active and logs `asset_kept`. The next step binds the new source to the same design. `metaldogs_assets_owner_ref_type_uidx` allows one live row per `(owner_ref, type)`, so that bind fails, the transaction rolls back, and the caller gets an error. Committing the replace while the protected row stayed live would either violate that index or leave the design pointing at the asset the replace meant to drop.

That path is not reachable from an order snapshot. A snapshot names only `artwork_asset_id` and `production_asset_id`, and `CommitOrder` moves the design to `ordered`. `ReplaceSource` runs only while `EditableForPatch` is true (`draft`, `generation_failed`, `preview_ready`). `softDeleteLiveAssetType` (artwork and mockup replace during generation) has the same index outcome if it leaves a live row of that `(owner_ref, type)`, and it runs on a design that is still generating or becoming preview-ready, before a snapshot exists. Clone copies the source photo onto a new asset id; it does not call `ReplaceSource` on the ordered design's artwork (`application/approve.go` cleanup, `DesignRepo.ReplaceSource`).

## Sweep (ticket 04)

`metaldogs:snapshots reconcile` prints one line per design line that has no snapshot: `order_id`, `line_key` (`order_id/variant_id`), `reason`. An unscoped run reads `OrderSource.ListCreatedSince` from `now - 30 days` (`ReconcileLookback`) and `Lines`. `--order` inspects that id through `Lines` and does not scan the list, including when the order is older than 30 days. A line that already has a snapshot is left out. `reason` is `missing_snapshot`, `snapshot_job_pending` while `snapshot_order_design` is pending, processing, or reclaiming, or `snapshot_job_failed:` plus the job's last error when that job is terminal-failed. A read error for one order is `inspect_failed` and the run continues with the other orders; the command still returns that error after the report. `--fix` enqueues `snapshot_order_design` with the UUID v5 job id and skips `inspect_failed`, so a second `--fix` does not add a job. With `plugins.metaldogs.order_snapshot.enabled` false the command still reports and returns an error on `--fix`. The CLI does not write snapshot rows or production columns.

Cron `reconcile_snapshots` (`@every 5m`, `integration.sync.metaldogs.reconcile_snapshots`) is registered only when that flag is on. It logs the same lines (`metaldogs.reconcile_snapshots.gap`) and does not insert snapshots or enqueue jobs. It uses the same 30-day window. While the flag is on, the cron calls `FillSnapshotProduction` for snapshots whose production columns are still NULL and whose design version is `ready`. A second fill is a no-op (`ErrProductionAlreadySet` on a direct second write). `Plugin.FillSnapshotProduction` is the other caller. `prepare_production_artwork` still returns `pending_spec` and does not set `production_status=ready`, so it does not call the fill. G5, when it marks a version `ready`, has to call `FillSnapshotProduction` for that version.

## Tests

Seam: run `snapshot_order_design` for an `order_id` on isolated Postgres with the in-memory `OrderSource`. No real `POST /api/v1/checkout`.

- `plugins/metaldogs/snapshot_order_test.go` — flag off registers nothing; flag on registers the manual job and the dispatcher; one design line copies the fields and sets `ordered`; two sizes, two rows, one status change; second run is one snapshot; line without a design id is skipped; missing design and missing approved version write nothing; template republish leaves checksums; production NULL then one fill; duplicate enqueue is one job; missing design retries to `failed`; duplicate `order.created` is one snapshot job; unbound source ends `failed`; commit failure names the design; soft-deleted artwork or production fails the commit
- `plugins/metaldogs/sync_snapshot_order_test.go` — UUID v5 job id; rollback logs every design line
- `plugins/metaldogs/reconcile_snapshots_test.go` — cron registered only when the flag is on; report, `--order`, `--fix` enqueues one job, flag off refuses `--fix`, production fill once
- `plugins/metaldogs/application/reconcile_snapshots_test.go` — window, direct inspect, pending
- `plugins/metaldogs/adapters/postgres/order_design_snapshot_repo_test.go` — `015` applied after `014`; second change to a non-production column fails; second production write fails
- `plugins/metaldogs/adapters/postgres/retention_test.go` — referenced assets survive each deletion path; a row with no snapshot is still deleted; share lock then keep
- `plugins/metaldogs/domain/order_design_snapshot_test.go` — copy and one production fill
- `plugins/metaldogs/application/snapshot_order_test.go` — failing design is named; clock required

## Flags / migrations / rollback

- `plugins.metaldogs.order_snapshot.enabled` default false. Documented in `configs/config.example.product.yaml`. Env `SHOPANDA_PLUGINS_METALDOGS_ORDER_SNAPSHOT_ENABLED`.
- Migration `015` is expand-only. Rollback: leave the flag false. The table stays. Do not delete snapshot rows; the trigger refuses it.
- Logs are order id, variant id, design id. No customer data, no image bytes, no signed URLs.

## Documentation updated

- [x] `docs/ROADMAP.md`: E4a **DONE** (CR-32). E4b listed as blocked on `OrderPort` lines/listing, `order.created` after the extension copy, and a pin bump. Known limits written. §1 rows for the extension copy and `order.created` match the pin.
- [x] `docs/architecture/flows/checkout-snapshot-and-fulfillment.md`: delivery (event, one job per order, sweep), line key `(order_id, variant_id)`, event-before-copy caveat, two Shopanda changes. No claim that a real order is snapshotted.
- [x] `docs/architecture/domain-model.md`: snapshot columns and the pair key. `docs/architecture/flows/design-to-order.md`: `ordered` keeps `409`; re-order is a clone.
- [x] `docs/architecture/api.md`: add-to-cart does not read or write the order snapshot; `ordered` stays `409`.
- [x] ADR-0005 status: implemented in E4a, not active (flag off, no real order source). Decision 4 states the line key. `docs/adr/README.md` matches.
- [x] `docs/runbooks/order-without-design-snapshot.md`: `metaldogs:snapshots reconcile`, `--fix`, `--order`, `jobs:list` types, terminal-failed means nothing written and the order is untouched, never insert rows by hand. Last exercised: never. `docs/runbooks/asset-retention-and-deletion.md`: the helper, unscheduled, after-`shipped` still O2.
- [x] `plugins/metaldogs/README.md` and `docs/runbooks/README.md`. `AGENTS.md` already lists `metaldogs:snapshots reconcile`. `docs/README.md` and `storefront/README.md` unchanged.
- [x] Safe delivery: flag off. Privacy: ids only on the snapshot logs.

## Follow-ups

- E4b: adapter and one real checkout test, after the pin that contains both Shopanda changes. Flag stays off until that adapter is bound.
- G5: call `FillSnapshotProduction` when a version becomes `ready`.
- O2: schedule the three deletion functions and decide the after-`shipped` grace.
- O5: exercise `docs/runbooks/order-without-design-snapshot.md` and replace "Last exercised: never".
- E5 / E6: admin read and confirmation thumbnail. Out of E4a.
