# Production file specification (P0)

**Proposed. Not a decision.** Facts dated 2026-11-25, to verify in the provider dashboards or with support.

No provider is chosen. The fulfilment rows in [ADR-0009](../adr/0009-pod-provider-and-catalog-modelling.md) stay `_pending_` until a sample is reviewed. This repository holds no printer numbers until P0 records them here.

The 2026-11-25 read used search snippets. Some vendor pages returned 403. It did not record a source host except `api.sandbox.prodigi.com` (Prodigi sandbox). Every provider cell below is that read: date 2026-11-25, source host `not confirmed`, unless the cell names that host. A value is not a dashboard confirmation.

## Selection requirements

Maintainer requirements for the sample and for any later choice. These are not vendor claims.

- Market: EU, including Poland. Production and shipping from the EU.
- Print: DTG, full colour, transparent PNG, sRGB.
- API: order creation, idempotency or an external id, webhooks, a cancellation window, and a sandbox or draft orders.
- Packaging: neutral (no provider brand the customer would see).
- Data processing agreement with an EU party.
- Sample budget: about 120 PLN.

## Candidates

Same garment for the two sample orders: Bella+Canvas 3001, so the comparison is the print. The 2026-11-25 read ranks Gelato ahead of Printful for that sample because it describes production in Poland and Germany for this shirt. That ranking is not a selection. Both are still to sample. Prodigi and Printify are out of this sample.

### Gelato — to sample

| Criterion | Value | Date | Source host |
| --- | --- | --- | --- |
| DTG, white underbase | yes (automatic) | 2026-11-25 | not confirmed |
| File | PNG, sRGB, 150 minimum / 300 DPI | 2026-11-25 | not confirmed |
| Production in the EU | local to the ship-to address; Poland and Germany for Bella+Canvas 3001 | 2026-11-25 | not confirmed |
| Order creation | `POST v4/orders`, `orderReferenceId` | 2026-11-25 | not confirmed |
| Cancellation | endpoint exists; window unclear (409 after the print has started) | 2026-11-25 | not confirmed |
| Webhooks | 4 events; signature not confirmed | 2026-11-25 | not confirmed |
| Sandbox | none; draft orders | 2026-11-25 | not confirmed |
| Sample price | from about 9.78 EUR net; discount applied automatically; shipping to Poland not confirmed | 2026-11-25 | not confirmed |
| Data-processing agreement | not confirmed (API Terms mention a DPA) | 2026-11-25 | not confirmed |
| Neutral packaging | labels and inserts; whether the standard plan includes them not confirmed | 2026-11-25 | not confirmed |

### Printful — to sample

| Criterion | Value | Date | Source host |
| --- | --- | --- | --- |
| DTG, white underbase | yes | 2026-11-25 | not confirmed |
| File | PNG, sRGB, 150–300 DPI | 2026-11-25 | not confirmed |
| Production in the EU | Riga (LV), Barcelona (ES) | 2026-11-25 | not confirmed |
| Order creation | `POST /orders`, `external_id` (max 32 characters) | 2026-11-25 | not confirmed |
| Cancellation | draft and pending only | 2026-11-25 | not confirmed |
| Webhooks | HMAC-SHA256 (header) | 2026-11-25 | not confirmed |
| Sandbox | webhook simulator only; draft order | 2026-11-25 | not confirmed |
| Sample price | Bella+Canvas 3001 about 11.9 USD, 20% discount, shipping in Europe 4.99 USD | 2026-11-25 | not confirmed |
| Data-processing agreement | published; processor; SCC | 2026-11-25 | not confirmed |
| Neutral packaging | no Printful brand; own packing slip | 2026-11-25 | not confirmed |

### Prodigi — excluded

No confirmed EU apparel lab. The read places shirts in the UK and the US (duty and transit). A sandbox and an idempotency key do not offset that for this sample.

| Criterion | Value | Date | Source host |
| --- | --- | --- | --- |
| DTG, white underbase | DTG; underbase not confirmed | 2026-11-25 | not confirmed |
| File | PNG/PDF, 300 DPI | 2026-11-25 | not confirmed |
| Production in the EU | shirts from the UK and the US; no confirmed EU lab | 2026-11-25 | not confirmed |
| Order creation | `POST /v4.0/orders`, idempotency header (optional) | 2026-11-25 | not confirmed |
| Cancellation | unclear; the read found contradictory docs | 2026-11-25 | not confirmed |
| Webhooks | CloudEvents; signature not confirmed | 2026-11-25 | not confirmed |
| Sandbox | yes (`api.sandbox.prodigi.com`) | 2026-11-25 | `api.sandbox.prodigi.com` |
| Sample price | shipping not confirmed; 50% refund not confirmed for apparel | 2026-11-25 | not confirmed |
| Data-processing agreement | published; artwork retention 30 vs 45 days (the read found both) | 2026-11-25 | not confirmed |
| Neutral packaging | no brand on some products | 2026-11-25 | not confirmed |

### Printify — excluded

Quality depends on the third-party print provider. The read found no sample discount. The EU contracting party on the data-processing agreement is not confirmed.

| Criterion | Value | Date | Source host |
| --- | --- | --- | --- |
| DTG, white underbase | yes | 2026-11-25 | not confirmed |
| File | PNG/JPEG/SVG, 300 DPI | 2026-11-25 | not confirmed |
| Production in the EU | providers in DE, LV, PL, ES; names not confirmed | 2026-11-25 | not confirmed |
| Order creation | `POST orders.json`, `external_id` (duplicate returns 409) | 2026-11-25 | not confirmed |
| Cancellation | on-hold and payment-not-received only | 2026-11-25 | not confirmed |
| Webhooks | HMAC-SHA256 (header) | 2026-11-25 | not confirmed |
| Sandbox | not confirmed | 2026-11-25 | not confirmed |
| Sample price | standard price; no sample discount | 2026-11-25 | not confirmed |
| Data-processing agreement | published; EU party not confirmed | 2026-11-25 | not confirmed |
| Neutral packaging | partial; depends on the provider | 2026-11-25 | not confirmed |

## Mapping to `production_spec`

`plugins.metaldogs.production_spec` is configuration for `prepare_production_artwork` ([CR-36](../cr-rounds/CR-36-g5a-production-artwork.md)). One file per design version. Per-size files stay out of scope (G5a).

A reviewed sample is what fills the keys. Until then every value stays empty. Do not copy example numbers from `configs/config.example.product.yaml`; those are not a printer size. This repository holds no printer numbers until P0 records them in the value column.

| Key | How a reviewed sample fills it | Value |
| --- | --- | --- |
| `width_px` | Pixel width of the print area that passed the placement check | |
| `height_px` | Pixel height of that area | |
| `dpi` | DPI of the file that passed | |
| `require_transparency` | `true` when that file is a transparent PNG and the underbase check passed | |
| `color_profile` | `srgb` only. The plugin rejects any other profile | |
| `upscale_policy` | `reject` or `upscale`, after the review says whether the passing file was already at print size | |

`color_profile` and `upscale_policy` are the sets the plugin accepts. They are not measured printer facts.

## Sample plan

One G1 test artwork for every shirt: transparent PNG, 300 DPI. The artwork includes fine detail, a semi-transparent gradient, saturated colours, and small text. No pixel size is fixed here.

Place two orders (Gelato and Printful) of Bella+Canvas 3001. If the budget allows, one dark and one light shirt colour per provider. Otherwise dark only.

Checks after delivery. Compare the shirt to the file and to the preview, not to a number this document does not contain.

| Check | Pass | Fail |
| --- | --- | --- |
| Print area and placement | The printed region matches the file's opaque bounds and sits on the ordered print area | Cropped, shifted off that area, or missing part of the file |
| White underbase and edge halo | On a dark shirt, colour sits on a solid white underbase with no white fringe outside the art | Colour looks thin on dark fabric, or a white halo outlines the art |
| Colour versus preview | Side-by-side with the storefront preview, hue and saturation would not mislead a buyer (S4 wording: preview vs production) | A buyer would reject the shirt as a different colour from the preview |
| Fine detail | Small text and fine lines from the file stay readable | They close up, drop out, or blur past recognition |
| Wash / feel | Qualitative: one gentle wash does not crack, peel, or make the shirt unwearable. Not a durability study | The print cracks or peels, or the hand-feel is unacceptable |
| Delivery time and cost | Produced and shipped from the EU to Poland. Days and PLN are written in the result table. The set fits about 120 PLN | Origin outside the EU, or the maintainer cannot place the planned shirts inside that budget |
| Packaging and branding | Parcel, insert, and label show no provider brand | A customer would see the provider's brand |

### Result

Empty until the shirts arrive. One row per check per shirt.

| Provider | Shirt colour | Check | Pass/fail | Days | PLN | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| | | | | | | |

## Vendor questions

Ask before paying for a sample. Answers are not in the 2026-11-25 read.

### Gelato

- Idempotency key, or deduplication of `orderReferenceId`.
- Cancellation deadline, and which status still allows it.
- Webhook signature and retries.
- Sandbox, or draft orders only.
- Signed DPA, EU party, artwork retention.
- Transit time and shipping cost to Poland, Germany, France, and Spain for Bella+Canvas 3001.
- Maximum file size and the print area in pixels.
- Neutral packaging and packing slip on the standard plan.
- Current sample discount.

### Printful

- Which facility serves Poland, Germany, France, and Spain, and the real transit times.
- Sandbox.
- Artwork retention and deletion on request.
- Shipping cost to Poland in EUR, and the account discount.
- Print area in pixels for Bella+Canvas 3001.
- Webhook retry policy.

## Design note for P2

Not implemented in this document.

A 32-character hex UUIDv5 of `(order_id, variant_id)` fits Gelato `orderReferenceId` and Printful `external_id` (max 32 in the 2026-11-25 read; source host not confirmed). P2 owns that identifier. The same read found no real sandbox at either candidate (draft orders, and a Printful webhook simulator) and a short or unclear cancellation window at both. Those are notes for P2, not behaviour in this tree.
