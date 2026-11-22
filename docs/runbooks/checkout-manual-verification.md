# Runbook: guest checkout manual verification

- **Introduced by slice:** `E3`
- **Last exercised:** never
- **Severity when triggered:** low
- **Owner:** maintainer

This is a checklist, not a recorded result. Do not mark Stripe or the SSR pages verified until the record section below is filled in. Automated coverage is `TestCheckout_DesignSale`: `POST /api/v1/checkout`, manual payment, no network, no Stripe keys.

## Before you start

- Plugin on: `SHOPANDA_PLUGINS_METALDOGS_ENABLED=true` (or `plugins.metaldogs.enabled: true`).
- Custom tee `mdogs-tee` published, with stock. The E1 seed is draft and stock 0.
- Stripe test mode, both secrets set. YAML `secret_key` and `webhook_secret` are ignored.
  - `SHOPANDA_PAYMENT_STRIPE_ENABLED=true`
  - `SHOPANDA_PAYMENT_STRIPE_SECRET_KEY` — a Stripe test secret (`sk_test_…`)
  - `SHOPANDA_PAYMENT_STRIPE_WEBHOOK_SECRET` — the test endpoint secret (`whsec_…`). Startup rejects Stripe when either secret is empty.
- Webhooks have to reach this process. Start `stripe listen --forward-to localhost:<port>/api/v1/payments/webhook/stripe` and put the `whsec_…` it prints into `SHOPANDA_PAYMENT_STRIPE_WEBHOOK_SECRET` before starting the app. A different secret fails signature verification.
- Guest browser. No customer account. Do not paste keys, card numbers, client secrets, or owner tokens into the record.

## Known limit

The SSR pages do not collect or confirm a card. `POST /checkout/confirm` creates the order and a Stripe PaymentIntent and leaves the payment `pending`. `client_secret` is returned only by the JSON checkout API (`POST /api/v1/checkout`). No template under `themes/` and no storefront script reads it or loads Stripe.js. `themes/default/templates/checkout_payment.html` shows a disabled placeholder. The caption on that placeholder is not a card step.

## Happy path

1. Approve a design and add one size with `POST /api/customizer/designs/{id}/add-to-cart`. Open `/cart` as that guest.
2. Walk `/checkout/address`, then shipping, then `/checkout/payment`. Choose payment method `stripe` when more than one method is shown.
3. The payment template shows a disabled placeholder `4242 4242 4242 4242`. Leave it. It is not submitted.
4. Place the order (`POST /checkout/confirm`).
5. Expected while the design is still `approved` and unexpired: an order exists, the confirmation notice is "Your order is created and Stripe payment confirmation is still required.", and `payments.status` for that order is `pending`. The PaymentIntent is unconfirmed. Record the order id. The PaymentIntent id is `payments.provider_ref` (also listed on the Stripe test dashboard). Do not record the client secret or the secret key.
6. Confirm that PaymentIntent outside the SSR pages, on the same test account as the secret key:

   `stripe payment_intents confirm <pi_id> --payment-method=pm_card_visa`

   The intent is visible in the Stripe test dashboard. This checklist uses the CLI command. A dashboard control that attaches a card was not checked on this pin.

7. Expected after `payment_intent.succeeded` is accepted at `POST /api/v1/payments/webhook/stripe`: `payments.status` is `completed`. That handler does not change the order status. Record the payment status.

## Design gone before pay

1. Add an approved design, then set that design's `expires_at` in the past before placing the order. Use expiry. A design from generate → approve has `metaldogs_generation_jobs` and `metaldogs_design_versions` rows. Both reference `metaldogs_designs` with no `ON DELETE CASCADE` (migrations 008 and 013), and `approved_version_id` references the version, so `DELETE FROM metaldogs_designs` fails. The feature test deletes a design that has neither dependent row.
2. Place the order from `/checkout/payment`.
3. Expected: the payment page shows "Design no longer available". The API contract for the same failure is `422` `validation` with that message and no line list. No order, no stock reservation.
4. A store outage on the same step is a different message: "Sorry, something went wrong. Please try again later." (`500` `internal` on `POST /api/v1/checkout`). That case is covered by the feature test. It is not required for this checklist.

## Record

Leave this empty until a person has done the steps. An empty record means not verified.

| Field | Value |
| --- | --- |
| Date | |
| Git revision | |
| Stripe mode | test |
| Happy-path order id | |
| Stripe payment-intent id | |
| Payment status after `payment_intent.succeeded` | |
| Expired design: page text | |
| Expired design: order created? | |
| Notes | |
