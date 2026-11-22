# CR-30: E3 checkout validation

- Status: **closed**
- Closed: 2026-11-22
- Slices: `E3`
- Milestone progress: M3 → E3

## What shipped

- Checkout step `metaldogs_design_sale` at `before:reserve_inventory`, registered only when the plugin is enabled
- One sale rule, `validateDesignLine`, used by `cart.validate` and by the step. `approved` or `ordered`, and not expired, passes. A missing row is not found (the design store has no soft-delete). Other loaded products are ignored. An unknown product fails closed
- A failing custom-tee line stops checkout before stock reservation and before the order row. Round 1 returned only the message `Design no longer available`. Round 2 returns the code, the lines, and `retryable`
- The add-to-cart route still accepts only `approved`
- No new migration and no new flag

## Round 2 (2026-11-22)

PR-1597 (`c25f3e7e`, shopanda#650) carries a plugin client code, `details`, and HTTP 503 on a checkout step error. The step now returns that instead of a message-only `422` and a masked `500`. PR-1598 adds SSR Stripe confirmation (`checkout_confirm`, POST then 303 then GET, `GET /checkout/payment/return`). The maintainer checklist is still empty, so this round does not record a Stripe test-mode run.

| HTTP | `error.code` | `error.message` | When |
| --- | --- | --- | --- |
| `201` | — | — | Sale check passes, or the cart has no custom-tee line. `data.order.id` is set |
| `422` | `plugin.metaldogs.design_unavailable` | `Design no longer available` | One or more custom-tee lines fail, or product classification is unknown. `details.lines` lists every `{variant_id, code}`. `details.retryable` is `false`. No order and no reservation |
| `503` | `plugin.metaldogs.validation_unavailable` | `Checkout validation unavailable` | Cart port, catalog, or design store cannot be read, or the cart id is missing. `details.retryable` is `true`. The cause is only in the log |

One refusal logs `metaldogs.checkout.design_refused` (cart id, variant ids, line codes). A logged-in cart is refused the same way as a guest cart. Captured bodies are in [`docs/architecture/api.md`](../architecture/api.md).

## Error contract (round 1, superseded)

Measured by `TestCheckout_DesignSale` (`POST /api/v1/checkout`, manual payment, isolated database) before PR-1597 was on this branch. Round 2 replaces this table.

| HTTP | `error.code` | `error.message` | When |
| --- | --- | --- | --- |
| `201` | — | — | Sale check passes, or the cart has no custom-tee line. `data.order.id` is set |
| `422` | `validation` | `Design no longer available` | One or more custom-tee lines fail, or product classification is unknown. Same body either way. No order and no reservation |
| `500` | `internal` | `internal server error` | Cart port, catalog, or design store cannot be read. No order and no reservation |

Round 1 could not return a plugin code or a line list, and SSR confirm did not exist yet. Both are closed by PR-1597 and PR-1598. The checklist still has to be filled by the maintainer. It does not claim that Stripe test mode was run.

## Verification results

| Item | Result |
| --- | --- |
| `bash .github/scripts/check-product-docs-links.sh` | Exit 0 on 2026-11-22 in this worktree |
| `TestCheckout_DesignSale` and `TestValidateDesignLine` | `TASK_DB=shopanda_task_e3 bash ~/ai-os/config/tests/metal_dogs.sh` on 2026-11-22: exit 0, 516 pass / 0 fail. Both PASS |
| Guest SSR checkout, Stripe test mode | Not run. Checklist [`docs/runbooks/checkout-manual-verification.md`](../runbooks/checkout-manual-verification.md) has an empty record |
| Known red in that gate | None. `ImportBoundary: 0 naruszeń (strict)`. `TestSmoke_OpenAI_Edits` skipped. `store_credit` and notification dedupe are outside this gate and were not re-run |

## Tests added

- `plugins/metaldogs/design_line_test.go` — shared rule, including `ordered` and a missing row
- `plugins/metaldogs/cart_validate_test.go`, `cart_validation_test.go` — hook uses the shared rule; an `ordered` design stays valid on the cart
- `plugins/metaldogs/checkout_sale_test.go` — `POST /api/v1/checkout`: approved, two sizes, `ordered`, expired, deleted, draft, `preview_ready`, missing design id, two bad lines, logged-in customer, another product, cart/catalog/design-store failures, plugin disabled. Refusals assert no new order and no stock change. `assertRefusalLogged` checks the structured refusal line (cart id, variant ids, line codes). A 503 body is `plugin.metaldogs.validation_unavailable` with `retryable: true` and does not contain the internal error text
- `plugins/metaldogs/cart_add_test.go` — add route still returns `409` `invalid_transition` for `ordered`

## Flags / migrations / rollback

- No new plugin migration and no new flag. The step is absent when `plugins.metaldogs.enabled` is false
- Rollback: disable the plugin or revert the change. Cart lines already written keep `metaldogs.design_id`

## Documentation updated

- [x] `docs/ROADMAP.md`: E3 **DONE** (CR-30). Manual verification called out as not recorded. Known limits listed. §1 extension-field row notes the checkout contract test and that SSR/Stripe are not recorded
- [x] `docs/architecture/api.md`: checkout status, plugin code, details.lines and retryable as measured (round 2)
- [x] `docs/architecture/flows/design-to-order.md`: step, shared validator, refusal before reserve. `checkout-snapshot-and-fulfillment.md` points at the same step
- [x] `docs/architecture/overview.md` and `domain-model.md`: unchanged. The step was already named. Checkout does not set `ordered`; E4 still does
- [x] No new ADR. ADR-0002 stays Proposed. Accepting it would treat SSR checkout as confirmed, and the checklist is empty
- [x] `docs/runbooks/checkout-manual-verification.md` and the runbook index. Empty record
- [x] `plugins/metaldogs/README.md` describes the shared validator and the step. `docs/cr-rounds/README.md` links this record. `AGENTS.md` commands unchanged
- [x] Safe delivery: plugin master switch. Carts with no custom-tee line are unchanged
- [x] Privacy/cost: no new logging of photos, tokens, or signed URLs. The checkout error body has no design id

## Accepted gaps

- Expiry between add and pay is unchanged (O2)
- Add-to-cart stays `approved`-only. Reorder of an `ordered` design through that route is an E4 decision
- Race between the step and order creation is E4
- Guest cart merge still copies `metaldogs.design_id` without hooks. The step is what catches it
- Stripe test mode on the SSR pages is not verified. SSR confirm exists upstream (PR-1598). The checklist record is empty

## Follow-ups

- Maintainer fills `docs/runbooks/checkout-manual-verification.md`
- E4: order snapshot, design → `ordered`, and the reorder decision for the add route
- O2: retention, including expiry while a design sits in a cart
- Storefront rendering of this error is the S track
