# 01: Hide or relabel metaldogs.design_id on order surfaces

**What to build:** Stop showing the internal design UUID as `Design ID: <uuid>` on the order confirmation email and on `checkout_confirm.html` and `account_order_detail.html`. The field was `VisibilityPublic` on `cart_item`, label `Design ID` (`plugins/metaldogs/design_field.go`). Checkout copies that value onto the order line. Pin `084d1206` renders every public `order_item` extension ([CR-38](../../../docs/cr-rounds/CR-38-upg12-pin-084d1206.md)). Chosen fix: `VisibilityPrivate`. E6b stays out.

**Blocked by:** None

**Status:** done

- [x] With the plugin enabled, a custom-tee line no longer shows an internal design UUID labeled `Design ID` on `order_confirmed`, checkout confirm, or account order detail
- [x] Cart attach and the checkout copy of `metaldogs.design_id` still succeed
- [x] E6b (`OrderThumbnailURL` on those same surfaces) stays out of this ticket
