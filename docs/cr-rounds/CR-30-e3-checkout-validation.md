# CR-30: E3 checkout validation

- Status: **closed**
- Closed: 2026-11-22
- Slices: `E3`
- Milestone progress: M3 → E3

## What shipped

- Checkout step `metaldogs_design_sale` at `before:reserve_inventory`, registered only when the plugin is enabled
- One sale rule, `ValidateDesignLine`, used by `cart.validate` and by the step. `approved` or `ordered`, and not expired, passes. A missing row is not found (the design store has no soft-delete). Other loaded products are ignored. An unknown product fails closed
- A failing custom-tee line stops checkout before stock reservation and before the order row. The client body is the message `Design no longer available`
- The add-to-cart route still accepts only `approved`
- No new migration and no new flag

## Error contract

Measured by `TestCheckout_DesignSale` (`POST /api/v1/checkout`, manual payment, isolated database). Written in [`docs/architecture/api.md`](../architecture/api.md).

| HTTP | `error.code` | `error.message` | When |
| --- | --- | --- | --- |
| `201` | — | — | Sale check passes, or the cart has no custom-tee line. `data.order.id` is set |
| `422` | `validation` | `Design no longer available` | One or more custom-tee lines fail, or product classification is unknown. Same body either way. No order and no reservation |
| `500` | `internal` | `internal server error` | Cart port, catalog, or design store cannot be read. No order and no reservation |

```text
PLATFORM_GAP
title: Checkout step error has no distinct code and no line list
blocking: no
where: plugins/metaldogs/checkout_sale.go designCheckoutStep.Execute
need: HTTP error body that keeps a plugin code (metaldogs.design_unavailable) and a per-line list (line or variant id plus design_required, design_not_found, design_expired, design_not_approved, product_unavailable)
proposal: extend ErrorBody with an optional details list, and let a non-500 code pass through JSONError unchanged
test: a checkout step returns that code and two line entries; POST /api/v1/checkout responds 422 with both fields still present
evidence: internal/platform/httpx/response.go ErrorBody is code+message; JSONError replaces any 500 with internal/internal server error. pkg/pluginsdk.ValidationError only takes a message. TestCheckout_DesignSale two bad lines asserts code validation and message Design no longer available.
END_PLATFORM_GAP
```

The step uses `pluginsdk.ValidationError` so the message survives. A custom code would hit the 500 replacement and the shopper would lose the sentence.

```text
PLATFORM_GAP
title: SSR checkout does not confirm the Stripe PaymentIntent
blocking: no
where: themes/default/templates/checkout_payment.html and checkout_confirm.html; internal/interfaces/http/storefront/storefront_checkout.go StartCheckout HTML path
need: the confirm page must receive client_secret and confirm the PaymentIntent in the browser. Today the secret is copied only into the JSON body of POST /api/v1/checkout
proposal: pass client_secret from the checkout context into the confirm template and confirm it with Stripe.js. Leave the PaymentIntent create call as it is
test: guest POST /checkout/confirm with method stripe renders a confirm step; after the test card, payment_intent.succeeded moves that payment from pending to completed
evidence: internal/interfaces/http/storefront/checkout.go:134 writes client_secret only on the JSON response. storefront_checkout.go:332 stores the order and the pending notice and does not read client_secret. themes/ has no client_secret and no Stripe.js. checkout_payment.html:66 is a disabled placeholder. stripepay.Provider.Initiate sets Pending and returns ClientSecret for a later Stripe.js confirm (provider.go:80).
END_PLATFORM_GAP
```

Workaround, not a core patch: the maintainer checklist confirms the PaymentIntent with `stripe payment_intents confirm <pi_id> --payment-method=pm_card_visa` and records `payments.status` after the webhook. A dashboard click that attaches a card was not verified.

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
- `plugins/metaldogs/checkout_sale_test.go` — `POST /api/v1/checkout`: approved, two sizes, `ordered`, expired, deleted, draft, `preview_ready`, missing design id, two bad lines, another product, cart/catalog/design-store failures, plugin disabled. Refusals assert no new order and no stock change
- `plugins/metaldogs/cart_add_test.go` — add route still returns `409` `invalid_transition` for `ordered`

## Flags / migrations / rollback

- No new plugin migration and no new flag. The step is absent when `plugins.metaldogs.enabled` is false
- Rollback: disable the plugin or revert the change. Cart lines already written keep `metaldogs.design_id`

## Documentation updated

- [x] `docs/ROADMAP.md`: E3 **DONE** (CR-30). Manual verification called out as not recorded. Known limits listed. §1 extension-field row notes the checkout contract test and that SSR/Stripe are not recorded
- [x] `docs/architecture/api.md`: checkout status, code, and body as measured, including the missing line list
- [x] `docs/architecture/flows/design-to-order.md`: step, shared validator, refusal before reserve. `checkout-snapshot-and-fulfillment.md` points at the same step
- [x] `docs/architecture/overview.md` and `domain-model.md`: unchanged. The step was already named. Checkout does not set `ordered`; E4 still does
- [x] No new ADR. ADR-0002 stays Proposed. Accepting it would treat SSR checkout as confirmed, and the checklist is empty
- [x] `docs/runbooks/checkout-manual-verification.md` and the runbook index. Empty record
- [x] `plugins/metaldogs/README.md` describes the shared validator and the step. `docs/cr-rounds/README.md` links this record. `AGENTS.md` commands unchanged
- [x] Safe delivery: plugin master switch. Carts with no custom-tee line are unchanged
- [x] Privacy/cost: no new logging of photos, tokens, or signed URLs. The checkout error body has no design id

## Accepted gaps

- HTTP body is the message only. See the `PLATFORM_GAP` block above (`blocking: no`)
- Expiry between add and pay is unchanged (O2)
- Add-to-cart stays `approved`-only. Reorder of an `ordered` design through that route is an E4 decision
- Race between the step and order creation is E4
- Guest cart merge still copies `metaldogs.design_id` without hooks. The step is what catches it
- Stripe test mode and the SSR pages are not verified. The pages create a pending PaymentIntent and do not confirm a card. See the second `PLATFORM_GAP` (`blocking: no`) and the checklist

## Follow-ups

- Maintainer fills `docs/runbooks/checkout-manual-verification.md`
- E4: order snapshot, design → `ordered`, and the reorder decision for the add route
- O2: retention, including expiry while a design sits in a cart
- Storefront rendering of this error is the S track
