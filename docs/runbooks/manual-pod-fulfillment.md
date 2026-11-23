# Runbook: manual print-on-demand fulfilment

- **Introduced by slice:** `P1` (procedure). Artwork fetch: `E5` ([CR-35](../cr-rounds/CR-35-e5-artwork-access.md))
- **Last exercised:** never
- **Severity when triggered:** low (routine at M3; fallback later)
- **Owner:** store operator

## Purpose

Until `P2` automates submission (and whenever automation is disabled or fails), orders are fulfilled by hand. This runbook is the M3 operating procedure.

## Fetch artwork (E5)

Requires `plugins.metaldogs.order_snapshot.enabled` true (default false; same flag as the snapshot jobs). While it is false the route, the permission, and `metaldogs:order-artwork` are not registered. Calling the handler anyway exits non-zero: `metaldogs:order-artwork: plugins.metaldogs.order_snapshot.enabled is false`.

Both surfaces call `OrderArtworkAccess`. Each issued URL is one row in `metaldogs_artwork_access_log` (migration `016`): `order_id`, `variant_id`, `kind` (`approved` or `production`), `actor`, `channel` (`admin` or `cli`), `requested_at`. The table is append-only. A row has no URL and no storage key. A failed log write returns no URL. The process log for the call is `metaldogs.order_artwork.access` with `order_id` and `actor` only. Do not paste the signed URL into a ticket.

TTL is fixed at 300 seconds (`OrderArtworkTTL`), under the 15-minute `MaxSignedGetTTL` cap. `expires_at` is in the JSON. After that the URL no longer fetches the bytes. Approved artwork is inline for `.jpg`, `.jpeg`, `.png`, `.webp`. The production URL is always a download, including when the file is a `.png`. The body never contains a storage key. `metaldogs.order_artwork.access` at error means signing or the audit write failed. A non-UUID order id is the same event at info, and the command prints `order id is not a uuid`.

Permission `metaldogs.snapshots.read` is registered for role `admin` only, and only while the flag is on.

Admin (permission-checked):

```bash
# Bearer of an admin account. Actor stored = that account's user id
# (pluginsdk.CustomerID). On /api/v1/admin a staff Bearer is the identity;
# a storefront session cookie does not replace it.
curl -sS -H "Authorization: Bearer $ADMIN_TOKEN" \
  "https://$HOST/api/v1/admin/orders/$ORDER_ID/artwork"
```

| HTTP | `error.code` | Meaning |
| --- | --- | --- |
| 200 | — | `{order_id, lines:[…]}`. `production` is `null` when that snapshot line has no production asset. An order that exists and has no snapshot lines is `200` with `lines: []` |
| 400 | `plugin.metaldogs.invalid_order_id` | Order id is not a UUID |
| 401 | `unauthorized` | No login. Platform code, from `RequirePermission`, before the handler |
| 403 | `forbidden` | Logged in, role lacks `metaldogs.snapshots.read` (only `admin` has it) |
| 404 | `plugin.metaldogs.order_not_found` | `Host.Orders().FindByID` has no such order. No audit row |
| 503 | `plugin.metaldogs.artwork_unavailable` | Signing or the audit write failed. No URL in the body |

Shell (no permission check; shell access is the trust boundary; still audited, actor `cli`, channel `cli`):

```bash
./app metaldogs:order-artwork "$ORDER_ID"
```

Stdout is the same JSON. Unknown order prints `not found` and exits non-zero. A non-UUID prints `metaldogs:order-artwork: order id is not a uuid`. Compare `production.checksum` (or `approved.checksum` when production is null) with the downloaded file. Delete the file from the workstation after upload.

## Daily procedure

1. List paid orders awaiting fulfilment: `TBD (P1)` command `metaldogs:fulfillment list --status=pending` (shows order id, variant id, variant SKU, production artwork status). The line key is `(order_id, variant_id)`. The snapshot table has no surrogate id.
2. For each line:
   1. Confirm `production_status = ready`. If `failed`, see [`generation-job-stuck-or-failed.md`](generation-job-stuck-or-failed.md) step 6. If `pending`/`preparing` for longer than expected, check the `integration.sync.metaldogs.prepare_production_artwork` job. If the snapshot line has no production asset, the E5 response has `production: null` — prepare the file before printing.
   2. Download the production file with the E5 command or route above. Verify the checksum in the JSON matches the downloaded file.
   3. Visually check the production file against the approved artwork URL from the same response (same artwork, correct orientation, nothing cropped). There is no admin theme page for this.
   4. Create the order in the POD provider's dashboard: garment, size from SKU, print area per `docs/architecture/production-spec.md`, shipping address from the Shopanda order.
   5. Record the external order id: `TBD (P1)` command `metaldogs:fulfillment record <order_id> <variant_id> --external-id=<id>` → status `submitted_manually`. Idempotent on `(order_id, variant_id)`; repeating that pair is a no-op.
3. When the provider reports shipping, record tracking: `TBD (P1)` command `metaldogs:fulfillment ship <order_id> <variant_id> --tracking=<n>` → status `shipped`; Shopanda shipment is updated and the customer notified (`P3` may automate).

## Rules

- Never upload a preview/mockup to the printer. Only the production asset referenced by the snapshot.
- Delete the downloaded production file from your machine after upload.
- Do not use customer photos for anything other than this order.

## Verification

- `metaldogs:fulfillment list --status=pending` empty at end of day; each shipped order has tracking in Shopanda.

## Prevention / follow-up

- `P2` automates steps 2.iv–2.v behind a manual gate; this runbook remains the fallback.
