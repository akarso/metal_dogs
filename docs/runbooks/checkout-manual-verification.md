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
- Guest browser. No customer account. Do not paste keys, card numbers, or owner tokens into the record.

## Happy path

1. Approve a design and add one size with `POST /api/customizer/designs/{id}/add-to-cart`. Open `/cart` as that guest.
2. Walk `/checkout/address`, then shipping, then `/checkout/payment`. Choose payment method `stripe` when more than one method is shown.
3. The payment template shows a disabled placeholder `4242 4242 4242 4242`. The caption says card entry happens on Stripe after the order is created. That field is not the charge.
4. Place the order (`POST /checkout/confirm`).
5. Expected when the design is still `approved` and unexpired: an order exists, and the confirmation notice is "Your order is created and Stripe payment confirmation is still required." Complete the card on Stripe's own step with Stripe's published test card `4242 4242 4242 4242`, any future expiry, any CVC. Expected after that step: Stripe reports the test payment succeeded. Record the order id and the Stripe payment-intent id. Do not record the card or the secret key.

## Design gone before pay

1. Add an approved design, then either delete that design row or set `expires_at` in the past before placing the order.
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
| Expired or deleted design: page text | |
| Expired or deleted design: order created? | |
| Notes | |
