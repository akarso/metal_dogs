# Runbook: guest checkout manual verification

- **Introduced by slice:** `E3`
- **Last exercised:** never
- **Severity when triggered:** low
- **Owner:** maintainer

This is a checklist, not a recorded result. Do not mark Stripe or the SSR pages verified until the record section below is filled in. Automated coverage is `TestCheckout_DesignSale`: `POST /api/v1/checkout`, manual payment, no network, no Stripe keys.

## Before you start

- Plugin on: `SHOPANDA_PLUGINS_METALDOGS_ENABLED=true` (or `plugins.metaldogs.enabled: true`).
- Custom tee `mdogs-tee` published, with stock. The E1 seed is draft and stock 0.
- Stripe test mode. YAML `secret_key` and `webhook_secret` are ignored.
  - `SHOPANDA_PAYMENT_STRIPE_ENABLED=true`
  - `SHOPANDA_PAYMENT_STRIPE_SECRET_KEY` — a Stripe test secret (`sk_test_…`)
  - `SHOPANDA_PAYMENT_STRIPE_WEBHOOK_SECRET` — the test endpoint secret (`whsec_…`). Startup rejects Stripe when either secret is empty.
  - `SHOPANDA_PAYMENT_STRIPE_PUBLISHABLE_KEY` — a test publishable key (`pk_test_…`). Without it the confirm page does not mount Stripe.js.
- Webhooks have to reach this process. Start `stripe listen --forward-to localhost:<port>/api/v1/payments/webhook/stripe` and put the `whsec_…` it prints into `SHOPANDA_PAYMENT_STRIPE_WEBHOOK_SECRET` before starting the app. A different secret fails signature verification.
- Guest browser. No customer account. Do not paste keys, card numbers, client secrets, or owner tokens into the record.

## Known limit

SSR confirm exists upstream (PR-1598, `themes/default/templates/checkout_confirm.html`). `POST /checkout/confirm` creates the order, stores a flash, and responds 303 to `GET /checkout/confirm`. That page loads Stripe.js when the publishable key, the PaymentIntent client secret, and `server.public_base_url` are set. Pay now calls `stripe.confirmPayment`. The return URL is `{public_base_url}/checkout/payment/return`. This checklist has not been walked. An empty record below means Stripe test mode is not verified.

## Happy path

1. Approve a design and add one size with `POST /api/customizer/designs/{id}/add-to-cart`. Open `/cart` as that guest.
2. Walk `/checkout/address`, then shipping, then `/checkout/payment`. Choose payment method `stripe` when more than one method is shown.
3. Place the order (`POST /checkout/confirm`). The browser should follow the 303 to `GET /checkout/confirm`.
4. Expected while the design is still `approved` and unexpired: an order exists, the confirmation page shows the Payment Element, and `payments.status` for that order is `pending` until the card is confirmed. Record the order id. The PaymentIntent id is `payments.provider_ref`. Do not record the client secret, the publishable key, or the secret key.
5. On that page, submit Pay now with Stripe test card `4242 4242 4242 4242`. `stripe.confirmPayment` runs in the browser. If Stripe redirects, the return route is `GET /checkout/payment/return`.
6. Expected after `payment_intent.succeeded` is accepted at `POST /api/v1/payments/webhook/stripe`: `payments.status` is `completed`. That handler does not change the order status. Record the payment status.

## Design gone before pay

1. Add an approved design, then set that design's `expires_at` in the past before placing the order. Use expiry. A design from generate → approve has `metaldogs_generation_jobs` and `metaldogs_design_versions` rows. Both reference `metaldogs_designs` with no `ON DELETE CASCADE` (migrations 008 and 013), and `approved_version_id` references the version, so `DELETE FROM metaldogs_designs` fails. The feature test deletes a design that has neither dependent row.
2. Place the order from `/checkout/payment`.
3. Expected: the payment page shows "Design no longer available". The API contract for the same failure is `422` `plugin.metaldogs.design_unavailable` with `details.lines` and `retryable: false`. No order, no stock reservation.
4. A store outage on the same step is "Checkout validation unavailable" (`503` `plugin.metaldogs.validation_unavailable`, `retryable: true` on `POST /api/v1/checkout`). The cause is not shown. That case is covered by the feature test. It is not required for this checklist.

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
