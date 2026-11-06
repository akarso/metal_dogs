# Runbook: asset retention sweep and deletion requests

- **Introduced by slice:** `O2`
- **Last exercised:** never
- **Severity when triggered:** medium (privacy obligation) / high if the sweep deletes something it should not
- **Owner:** on-call operator; data-protection contact for requests

## Purpose

The `integration.sync.metaldogs.retention_sweep` scheduler task applies the policy table in `docs/architecture/domain-model.md` to `metaldogs_assets` and `metaldogs_designs`: it deletes storage objects, marks asset rows `deleted_at`, and moves never-ordered designs to `expired`. Assets referenced by an `OrderDesignSnapshot` with incomplete fulfilment are never deleted. Deletions are logged with ids only.

## Routine checks

- Sweep runs on schedule (`./app schedule:list`), finishes within its window, and the deletion count is within the normal band. A sudden spike means a policy or clock bug: stop and check before the next run.
- `retention.dry_run` is `true` on first deploy of any policy change; review the "would delete" log before switching to `false`.

## Symptom: sweep failing

1. `./app schedule:list` and the scheduler logs for `integration.sync.metaldogs.retention_sweep`.
2. Typical causes: storage credentials, a storage object already missing (should be tolerated and logged, not fatal), long-running query.
3. Fix the cause; the sweep is idempotent and resumes next run. `./app schedule:trigger integration.sync.metaldogs.retention_sweep` to run immediately.

## Symptom: sweep deleted an ordered artwork

1. Treat as an incident. Set `retention.dry_run: true` immediately.
2. Identify affected snapshots by comparing `metaldogs_order_design_snapshots.artwork_asset_id` with `metaldogs_assets.deleted_at IS NOT NULL`.
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
