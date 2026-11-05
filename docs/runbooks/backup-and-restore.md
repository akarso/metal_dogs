# Runbook: backup and restore

- **Introduced by slice:** `O6`
- **Last exercised:** never
- **Severity when triggered:** high
- **Owner:** infrastructure owner

## Scope

Two stores must be restored consistently:

- PostgreSQL: Shopanda core tables, `metaldogs_*` tables, job queue.
- Object storage: the private Metal Dogs bucket (source photos, artwork, mockups, production files, template references).

The invariant to protect: for every `OrderDesignSnapshot`, the referenced approved and production artwork objects exist and match their checksums.

## Backup

- PostgreSQL: schedule and tooling per Shopanda's deployment guide (verify against pinned tag); retention of backups at least as long as the asset retention policy.
- Object storage: versioning enabled on the bucket plus lifecycle rules aligned with the retention policy; or periodic replication to a second bucket. Decision recorded in `O6`.
- Secrets (storage keys, provider keys, Stripe) are in the secret store, not in backups.

## Restore rehearsal (quarterly and before M3 launch)

1. Restore the database to a staging instance.
2. Restore or point at the restored bucket.
3. Run `TBD (O6)` command `metaldogs:integrity check-snapshots` — verifies every snapshot's assets exist and checksums match.
4. Run the E2E flow with the fake provider on the restored environment.
5. Record duration and gaps here.

## Real recovery

1. Stop `app serve`, `app worker`, `app scheduler` to avoid writes to a half-restored state.
2. Restore database, then storage (or vice versa — order does not matter if both are point-in-time consistent; if not, restore storage to a point **after** the database point so no referenced object is missing).
3. Run the integrity check; list snapshots with missing objects.
4. For missing objects within bucket versioning history, restore the version; otherwise mark the affected fulfilment records and handle customer-by-customer.
5. Start processes; watch the reconciliation sweeps; they will re-enqueue missed snapshot jobs.

Irreversible: overwriting the production database. Always restore to a new instance and switch.

## Verification

- Integrity check passes; a new order on staging produces a snapshot; queue processes.

## Prevention / follow-up

- Integrity check also runs nightly as a scheduler task and alerts.
