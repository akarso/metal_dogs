# Design ID on order surfaces

Pin `084d1206` ([CR-38](../../docs/cr-rounds/CR-38-upg12-pin-084d1206.md)) renders public `order_item` extensions on the confirmation email and the storefront order pages. `metaldogs.design_id` is public on `cart_item` with label `Design ID`, and checkout copies it onto the order line, so each custom-tee line shows `Design ID: <uuid>`.

This pin accepts that. E6b is the thumbnail call and does not include this change.

## Tickets

| # | Title | Status |
| --- | --- | --- |
| 01 | Hide or relabel `metaldogs.design_id` on order email and order pages | needs-triage |
