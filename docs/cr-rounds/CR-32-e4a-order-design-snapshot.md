# CR-32: E4a order design snapshot

- Status: **open** — ticket 04 added the reconcile sweep. Ticket 05 closes the round.
- Slices: `E4a`

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

`metaldogs:snapshots reconcile` prints one line per design line that has no snapshot: `order_id`, `line_key` (`order_id/variant_id`), `reason`. It reads `OrderSource.ListCreatedSince` and `Lines`. A line that already has a snapshot is left out. `reason` is `missing_snapshot`, or `snapshot_job_failed:` plus the job's last error when `snapshot_order_design` is terminal-failed. `--order` limits the report to one id. `--fix` enqueues `snapshot_order_design` with the UUID v5 job id, so a second `--fix` does not add a job. With `plugins.metaldogs.order_snapshot.enabled` false the command still reports and returns an error on `--fix`.

Cron `reconcile_snapshots` (`@every 5m`, `integration.sync.metaldogs.reconcile_snapshots`) is registered only when that flag is on. It logs the same lines (`metaldogs.reconcile_snapshots.gap`) and does not insert snapshots or enqueue jobs. While the flag is on, the CLI and the cron both call `FillSnapshotProduction` for snapshots whose production columns are still NULL and whose design version is `ready`. A second fill is a no-op (`ErrProductionAlreadySet` on a direct second write).
