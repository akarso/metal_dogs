# CR-32: E4a order design snapshot

- Status: **closed**
- Closed: 2026-11-23
- Slices: `E4a`
- Milestone progress: M3 → E4a

## What shipped

Plugin-side order snapshot. Dark. A real checkout does not write a row.

- Table `metaldogs_order_design_snapshots` (migration `015`). Primary key `(order_id, variant_id)`. Copy of the approved version: SKU, template slug and version, texts, provider, model, prompt version, artwork id and checksum, production id and checksum (nullable, set once from NULL). Deletes refused. Other columns refuse updates.
- `ports.OrderSource`: `Lines`, `ListCreatedSince`. E4a ships an in-memory fake used by tests. No adapter over `Host.Orders()`. No reads of core tables.
- Flag `plugins.metaldogs.order_snapshot.enabled`, default false. Off: `snapshot_order_design`, `dispatch_snapshot_order`, and `reconcile_snapshots` are not registered. With an `OrderSource` bound, `metaldogs:snapshots reconcile` reports and refuses `--fix`. E4a binds no source, so the command exits `order source is not bound` and prints nothing.
- On: `order.created` enqueues `dispatch_snapshot_order` (`RegisterOnEvent`). That job enqueues `snapshot_order_design` with `WithJobID` = UUID v5 of the canonical order id. One plugin transaction inserts every design line (`ON CONFLICT DO NOTHING`) and moves each design `approved → ordered` (already `ordered` is a no-op). A missing design, approved version, or pinned template fails the job, writes nothing, and leaves the Shopanda order untouched. Retries end `failed`.
- `metaldogs:snapshots reconcile` reports `order_id`, `line_key` (`order_id/variant_id`), `reason` only when an `OrderSource` is bound. Default window is 30 days. `--order` inspects one id. `--fix` enqueues one snapshot job per affected order and skips `inspect_failed`. Cron `reconcile_snapshots` (`@every 5m`) logs the same report as `metaldogs.reconcile_snapshots.gap` and does not enqueue. No alert is configured on that line. Production fill on that cron runs only with a bound source (E4b). In E4a the flag is off, so the cron is not registered. At this close, with the flag on and no source, that cron fails every run (`order source is not bound`), logs `metaldogs.reconcile_snapshots.failed`, emits no gap line, and fills nothing. Current boot behaviour is [CR-34](CR-34-e4b-order-source.md).
- `AssetsReferencedBySnapshots` is the deletion check on every plugin path that soft-deletes an asset. Nothing schedules those paths.

## Decisions

- Line key is `(order_id, variant_id)`. ADR-0005 decision 4 called it `order_line_id`. Shopanda order items have no id. The pair is the key core uses for `order_item` extension values.
- Re-order of an `ordered` design stays `409` `invalid_transition` on add-to-cart. The path is `CloneDraft`, then a new approval. `CloneDraft` from `ordered` already existed.
- E4a depends on a plugin-owned port so E4b only swaps the adapter.
- Production artwork copied when the version is `ready` at snapshot time; otherwise NULL, filled once later. G5 must call `(*metaldogs.Plugin).FillSnapshotProduction` when it sets `production_status=ready`. The prepare stub does not.

## Platform dependencies (closed on the pin by CR-33)

At E4a close these two gaps were still open. Pin `e062d4d3` ([CR-33](CR-33-upg11-pin-e062d4d3.md)) has both. E4b was not part of this round; it closed in [CR-34](CR-34-e4b-order-source.md). This round did not edit `internal/*`, `pkg/*`, or core `cmd/api/*`.

What was true at E4a close (pin `c25f3e7e`):

1. `pluginsdk.OrderPort` was `FindByID` only (`pkg/pluginsdk/ports.go`). E4b needed `Lines` (extension values) and `ListCreatedSince`.
2. `CreateOrderStep` called `publishOrderCreated` and then `SnapshotCartItemToOrderItem`. `order.created` therefore ran before `metaldogs.design_id` was on the order line.

On `e062d4d3`: `OrderPort` has `Lines`, `LinesForOrders`, and `ListCreatedSince`. `finishCreatedOrder` snapshots, then publishes. A snapshot error still publishes.

## Known limits

- At this round's close: flag off and no real order source. Tests are not a checkout. The adapter is [CR-34](CR-34-e4b-order-source.md).
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
| Compensating soft-delete | `AssetRepo.SoftDelete` (upload sign-failure cleanup, clone cleanup) | The asset id minted in that request, then its blob. `SoftDelete` failure leaves the blob | Same. A snapshot cannot name that id yet, so cleanup does not re-read the row |
| Replace source | `DesignRepo.ReplaceSource` → `softDeleteAsset` | Previous source asset and, when the row was marked deleted, its blob | Same. The replace transaction does not commit while the old row is still the live source |
| Replace live asset | `GenerationJobRepo.PersistArtwork`, `CompleteSuccess`, `FinalizeSuccess` → `softDeleteLiveAssetType` | Previous live asset of that owner and type, then its blob | Same |

Searched `deleted_at`, `delete_after`, and design expiry handling under `plugins/metaldogs`. Those three functions were not in the tree; this ticket added them so every deletion decision goes through one helper. Nothing schedules them. Those three are the only retention deletion paths. O2 calls them and does not add its own `UPDATE` of `deleted_at` on the same rows. Scheduling and the post-`shipped` policy stay in O2. `delete_after` updates that only move the clock are not deletion paths.

`partitionSnapshotAssets` takes `SELECT … FOR UPDATE` on the asset rows (`ORDER BY id`) before `AssetsReferencedBySnapshots`. Every deletion path calls that helper, including `AssetRepo.SoftDelete` and `softDeleteAsset` (`ReplaceSource`). `CommitOrder` and `FillProduction` take `FOR SHARE` on the live asset rows they are about to reference, in the same transaction as the write, so a snapshot commit cannot land between the check and `deleted_at`. A missing or already soft-deleted asset fails the snapshot write and leaves the order unchanged.

## Replace of a protected asset returns an error

`ReplaceSource` does not treat a protected source as a quiet skip. `softDeleteAsset` leaves the row active and logs `asset_kept`. The next step binds the new source to the same design. `metaldogs_assets_owner_ref_type_uidx` allows one live row per `(owner_ref, type)`, so that bind fails, the transaction rolls back, and the caller gets an error. Committing the replace while the protected row stayed live would either violate that index or leave the design pointing at the asset the replace meant to drop.

That path is not reachable from an order snapshot. A snapshot names only `artwork_asset_id` and `production_asset_id`, and `CommitOrder` moves the design to `ordered`. `ReplaceSource` runs only while `EditableForPatch` is true (`draft`, `generation_failed`, `preview_ready`). `softDeleteLiveAssetType` (artwork and mockup replace during generation) has the same index outcome if it leaves a live row of that `(owner_ref, type)`, and it runs on a design that is still generating or becoming preview-ready, before a snapshot exists. Clone copies the source photo onto a new asset id; it does not call `ReplaceSource` on the ordered design's artwork (`application/approve.go` cleanup, `DesignRepo.ReplaceSource`).

## Sweep (ticket 04)

When an `OrderSource` is bound, `metaldogs:snapshots reconcile` prints one line per design line that has no snapshot: `order_id`, `line_key` (`order_id/variant_id`), `reason`. An unscoped run reads `OrderSource.ListCreatedSince` from `now - 30 days` (`ReconcileLookback`) and `Lines`. `--order` inspects that id through `Lines` and does not scan the list, including when the order is older than 30 days. A line that already has a snapshot is left out. `reason` is `missing_snapshot`, `snapshot_job_pending` while `snapshot_order_design` is pending, processing, or reclaiming, or `snapshot_job_failed:` plus the job's last error when that job is terminal-failed. A read error for one order is `inspect_failed` and the run continues with the other orders; the command still returns that error after the report. `--fix` enqueues `snapshot_order_design` with the UUID v5 job id and skips `inspect_failed`, so a second `--fix` does not add a job. With an `OrderSource` bound and `plugins.metaldogs.order_snapshot.enabled` false, the command still reports and returns an error on `--fix`. Before E4b bound that source, `metaldogs:snapshots reconcile` (with or without `--order`) exited `order source is not bound` and printed nothing; `--fix` while the flag is false returns the refusal. The bound path is [CR-34](CR-34-e4b-order-source.md). The CLI does not write snapshot rows or production columns.

Cron `reconcile_snapshots` (`@every 5m`, `integration.sync.metaldogs.reconcile_snapshots`) is registered only when that flag is on. It logs the same lines (`metaldogs.reconcile_snapshots.gap`) and does not insert snapshots or enqueue jobs. No alert is configured on that line. It uses the same 30-day window. The cron calls `FillSnapshotProduction` only when `OrderSource` is bound, for snapshots whose production columns are still NULL and whose design version is `ready`. Without that source the job errors every run (`metaldogs.reconcile_snapshots.failed`) and fills nothing. In E4a the flag is off, so the cron is not registered. A second fill is a no-op (`ErrProductionAlreadySet` on a direct second write). `Plugin.FillSnapshotProduction` is the other caller. `prepare_production_artwork` still returns `pending_spec` and does not set `production_status=ready`, so it does not call the fill. G5, when it marks a version `ready`, has to call `FillSnapshotProduction` for that version.

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

- [x] `docs/ROADMAP.md`: E4a **DONE** (CR-32). The adapter slice stayed open at this close. [CR-34](CR-34-e4b-order-source.md) marks it done. Known limits written. §1 rows for the extension copy and `order.created` match the pin.
- [x] `docs/architecture/flows/checkout-snapshot-and-fulfillment.md` at this close: delivery (event, one job per order, sweep), line key `(order_id, variant_id)`, and the two Shopanda changes still open then. Current ordering and the adapter are [CR-34](CR-34-e4b-order-source.md). No claim that a real order is snapshotted while the flag is off.
- [x] `docs/architecture/domain-model.md`: snapshot columns and the pair key. `docs/architecture/flows/design-to-order.md`: `ordered` keeps `409`; re-order is a clone.
- [x] `docs/architecture/api.md`: add-to-cart does not read or write the order snapshot; `ordered` stays `409`.
- [x] ADR-0005 status at this close: implemented in E4a, flag off. Decision 4 states the line key. Current status (E4b binds `Host.Orders()` when the flag is on; the flag still defaults off) is [CR-34](CR-34-e4b-order-source.md). `docs/adr/README.md` matches that status.
- [x] `docs/runbooks/order-without-design-snapshot.md`: `metaldogs:snapshots reconcile`, `--fix`, `--order`, `jobs:list` types, terminal-failed means nothing written and the order is untouched, never insert rows by hand. Last exercised: never. `docs/runbooks/asset-retention-and-deletion.md`: the helper, unscheduled, after-`shipped` still O2.
- [x] `plugins/metaldogs/README.md` and `docs/runbooks/README.md`. The plugin README does not say the unbound reconcile command prints a report. The runbook signal is `metaldogs.reconcile_snapshots.gap`; no alert is configured. Fulfilment docs name the line key `(order_id, variant_id)`. `AGENTS.md` already lists `metaldogs:snapshots reconcile`. `docs/README.md` and `storefront/README.md` unchanged.
- [x] Safe delivery: flag off. Privacy: ids only on the snapshot logs.

## Follow-ups

- E4b: adapter and one real checkout test, after the pin that contains both Shopanda changes. Flag stays off until that adapter is bound. Done in [CR-34](CR-34-e4b-order-source.md).
- G5: call `FillSnapshotProduction` when a version becomes `ready`.
- O2: schedule `CleanupDueAssets`, `CleanupUnattachedUploads`, and `ExpireDesigns`. Those are the only retention deletion paths. Do not add a separate `UPDATE` of `deleted_at`. Decide the after-`shipped` grace.
- O5: exercise `docs/runbooks/order-without-design-snapshot.md` and replace "Last exercised: never".
- E6: confirmation thumbnail. Out of E4a. E5 artwork access closed in [CR-35](CR-35-e5-artwork-access.md).
