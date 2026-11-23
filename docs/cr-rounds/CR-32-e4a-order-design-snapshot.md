# CR-32: E4a order design snapshot

- Status: **open** — ticket 03 recorded the deletion paths. Ticket 05 closes the round.
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

Searched `deleted_at`, `delete_after`, and design expiry handling under `plugins/metaldogs`. The three retention functions were not in the tree; O2 still owns scheduling them. `delete_after` updates that only move the clock are not deletion paths.
