# Runbook: manual print-on-demand fulfilment

- **Introduced by slice:** `P1`
- **Last exercised:** never
- **Severity when triggered:** low (routine at M3; fallback later)
- **Owner:** store operator

## Purpose

Until `P2` automates submission (and whenever automation is disabled or fails), orders are fulfilled by hand. This runbook is the M3 operating procedure.

## Daily procedure

1. List paid orders awaiting fulfilment: `TBD (P1)` command `metaldogs:fulfillment list --status=pending` (shows order id, line, variant SKU, snapshot id, production artwork status).
2. For each line:
   1. Confirm `production_status = ready`. If `failed`, see [`generation-job-stuck-or-failed.md`](generation-job-stuck-or-failed.md) step 6. If `pending`/`preparing` for longer than expected, check the `integration.sync.metaldogs.prepare_production_artwork` job.
   2. Download the production file via `E5` (`TBD (E5)` command `metaldogs:order-artwork <order_id>` → short-lived signed URL). Verify the checksum printed by the command matches the downloaded file.
   3. Visually check the production file against the approved mockup in admin (same artwork, correct orientation, nothing cropped).
   4. Create the order in the POD provider's dashboard: garment, size from SKU, print area per `docs/architecture/production-spec.md`, shipping address from the Shopanda order.
   5. Record the external order id: `TBD (P1)` command `metaldogs:fulfillment record <snapshot_id> --external-id=<id>` → status `submitted_manually`. Idempotent; repeating with the same id is a no-op.
3. When the provider reports shipping, record tracking: `TBD (P1)` command `metaldogs:fulfillment ship <snapshot_id> --tracking=<n>` → status `shipped`; Shopanda shipment is updated and the customer notified (`P3` may automate).

## Rules

- Never upload a preview/mockup to the printer. Only the production asset referenced by the snapshot.
- Delete the downloaded production file from your machine after upload.
- Do not use customer photos for anything other than this order.

## Verification

- `metaldogs:fulfillment list --status=pending` empty at end of day; each shipped order has tracking in Shopanda.

## Prevention / follow-up

- `P2` automates steps 2.iv–2.v behind a manual gate; this runbook remains the fallback.
