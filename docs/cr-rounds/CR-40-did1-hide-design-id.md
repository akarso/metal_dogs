# CR-40: Hide metaldogs.design_id from shopper order surfaces

- Closed: 2026-11-25
- Slices: DID1
- Milestone progress: M3

## What shipped

`registerDesignIDField` sets `VisibilityPrivate`. Label, type, scope, snapshot storage, plugin-only write, and the UUID regex are unchanged.

Checkout still copies the value. `SnapshotCartItemToOrderItem` copies `StorageSnapshot` rows and does not read visibility. `POST /api/v1/checkout` still returns `201`. The public order body does not contain `metaldogs.design_id`. The `order_item` row does.

Shopper order JSON, the confirmation email, and `checkout_confirm.html` / `account_order_detail.html` use `PublicOrderItemAPIByVariant` (`includePrivate=false`), then `ExtensionDisplayRows`. A private field is absent from that list, so those surfaces omit the UUID.

## Consumers checked

- Public cart JSON (`List` with `includePrivate=false`) and `themes/default/templates/cart.html` no longer show the field. The customizer does not read cart extensions. No product caller needed the UUID on a public surface.
- An untrusted cart write of the field is `403` `forbidden_private_field` (private is checked before plugin-only). Nothing is stored. The add-to-cart route still writes through the trusted plugin context.
- `Host.Cart()` / `cart.validate` still see the owner's private `PluginOnlyWrite` value (PR-1606). Add-to-cart and checkout validation stay on that path.
- `Host.Orders()` still returns the owner's private value (`keepOrderExtension`). The snapshot job is unchanged.
- E6b stays out of scope.

## Tests added

- `TestPlugin_Init_Enabled_RegistersDesignIDField` expects `VisibilityPrivate`.
- Public cart reads fail if `metaldogs.design_id` is present. Stored value is read from `extension_values`.
- `assertOrderCarriesDesign` uses `POST /api/v1/checkout`: the response omits the field and the design UUID; the `order_item` row keeps the UUID.
- Existing cart, checkout, and snapshot tests stay in place.

## Flags / migrations / rollback

No migration and no flag. Rollback is reverting the visibility change. Rows already stored stay.

## Documentation updated

- [x] `docs/ROADMAP.md`: field is private; email and order pages omit it. E6b still does not call `OrderThumbnailURL`.
- [x] `docs/architecture/domain-model.md`, `docs/architecture/api.md`, `docs/architecture/overview.md`, `docs/architecture/flows/design-to-order.md`, `docs/architecture/flows/checkout-snapshot-and-fulfillment.md`
- [x] `docs/UPSTREAM_PIN.md`, `plugins/metaldogs/README.md`, this index
- [x] No new ADR. No Shopanda file change.
- [x] `.scratch/design-id-order-display` marked done

## Follow-ups

- E6b: register `order.line.display` and append `OrderThumbnailURL`. Pin `98d67491` ([CR-41](CR-41-upg14-pin-98d67491.md)) adds payload `language` (`OrderLineDisplayLanguage()`). E6b is not done.
