# Runbook: asset retention sweep and deletion requests

- **Introduced by slice:** `O2`
- **Last exercised:** never
- **Severity when triggered:** medium (privacy obligation) / high if the sweep deletes something it should not
- **Owner:** on-call operator; data-protection contact for requests

## Purpose

The `integration.sync.metaldogs.retention_sweep` scheduler task is still O2. It is not registered. The policy table in `docs/architecture/domain-model.md` is the target. E4a ([CR-32](../cr-rounds/CR-32-e4a-order-design-snapshot.md)) answers "may this asset be deleted?" with `AssetsReferencedBySnapshots`. A row in `metaldogs_order_design_snapshots` whose `artwork_asset_id`, `production_asset_id`, `mockup_asset_id`, or `thumbnail_asset_id` is that asset is left active and logged once as `metaldogs.retention.asset_kept` (`path`, `asset_id`). The mockup and thumbnail columns are E6a ([CR-37](../cr-rounds/CR-37-e6a-order-thumbnail.md), migration `018`). Fulfilment status is not consulted. The after-`shipped` grace stays in O2.

O2's sweep calls only `postgres.CleanupDueAssets`, `postgres.CleanupUnattachedUploads`, and `postgres.ExpireDesigns`. Those functions already skip snapshot-referenced assets. O2 does not add its own `UPDATE` of `deleted_at` on the same rows. With `plugins.metaldogs.order_snapshot.enabled` false, no snapshot rows are written, so this skip stays idle.

A thumbnail is stored before the snapshot transaction commits. If that line drops the thumbnail or the commit rolls back, the job deletes the object unless a live `metaldogs_assets` row still has that storage key. If that lookup or the delete fails, the object can remain with no snapshot reference. The sweep does not see it. The warning is `metaldogs.snapshot_order_design.thumbnail` (`order_id`, `variant_id` only).

## Routine checks

- Sweep runs on schedule (`./app schedule:list`), finishes within its window, and the deletion count is within the normal band. A sudden spike means a policy or clock bug: stop and check before the next run.
- `retention.dry_run` is `true` on first deploy of any policy change; review the "would delete" log before switching to `false`.

## Symptom: sweep failing

1. `./app schedule:list` and the scheduler logs for `integration.sync.metaldogs.retention_sweep`.
2. Typical causes: storage credentials, a storage object already missing (should be tolerated and logged, not fatal), long-running query.
3. Fix the cause; the sweep is idempotent and resumes next run. `./app schedule:trigger integration.sync.metaldogs.retention_sweep` to run immediately.

## Symptom: sweep deleted an ordered artwork

1. Treat as an incident. Set `retention.dry_run: true` immediately.
2. Identify affected snapshots by comparing `artwork_asset_id`, `production_asset_id`, `mockup_asset_id`, and `thumbnail_asset_id` on `metaldogs_order_design_snapshots` with `metaldogs_assets.deleted_at IS NOT NULL`.
3. Restore objects from backup ([`backup-and-restore.md`](backup-and-restore.md)) if within window.
4. Add a regression test reproducing the policy decision before re-enabling.

## Procedure: data deletion request (GDPR)

1. Receive the request through the Shopanda legal/GDPR flow (verified: Shopanda has GDPR export/delete). The plugin registers a handler for the customer-deletion event (`O2`; **verify** event name).
2. For a guest without a customer record, identify designs by information the requester can provide (order id, or the design id from their session). Never search by dog name or photo.
3. Run `TBD (O2)` command `metaldogs:privacy delete --design=<id>` or `--order=<id>`: deletes source photo and previews immediately; approved/production artwork tied to a fulfilled order is deleted after the legal retention period unless the requester insists and law allows — follow the policy decided in `O2`.
4. Record the request and completion with ids only.

## Verification

- Storage objects gone (`Head` returns not found); asset rows have `deleted_at`; design `expired`; snapshot row remains with ids.

## Prevention / follow-up

- Policy decision table unit tests must include "ordered, unfulfilled → keep" and "approved, within order grace window → keep".
