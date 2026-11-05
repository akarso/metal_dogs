# Runbook: automated POD submission failed or stale

- **Introduced by slice:** `P2`, `P4`
- **Last exercised:** never
- **Severity when triggered:** medium (order delayed; manual path available)
- **Owner:** on-call operator

## Symptom

- Alert: `submission_failed` count, or fulfilment records in `submitting`/`submitted` beyond the expected window, or POD webhook signature failures.
- Provider dashboard shows an order we do not have as `submitted`, or vice versa.

## Impact

Delayed or duplicate production. Duplicate is the costly case; the design is idempotent on `snapshot_id` and uses the provider idempotency key where supported.

## Diagnosis

1. `TBD (P2)` command `metaldogs:fulfillment show <snapshot_id>`: status, attempts, `last_error_code`, provider request id.
2. Classify `last_error_code`:
   - validation (file spec, address, SKU mapping) → permanent; fix data or spec.
   - auth/config → permanent; credentials.
   - 5xx/timeout → transient; bounded retries already ran.
3. Stale `submitting`: the worker may have crashed after the provider accepted. **Check the provider dashboard before resubmitting.**
4. Webhook signature failures: secret rotated on one side only, or replay attack; check the source.

## Remediation

1. Permanent validation error → correct the cause (e.g. address via Shopanda, SKU mapping via config) then `TBD (P2)` `metaldogs:fulfillment resubmit <snapshot_id>`.
2. Stale `submitting`, provider has the order → `TBD (P4)` `metaldogs:fulfillment attach <snapshot_id> --external-id=<id>` to move to `submitted` without a new order.
3. Stale `submitting`, provider has nothing → resubmit.
4. Provider outage → disable `fulfillment.auto_submit`; fulfil by hand if urgent ([`manual-pod-fulfillment.md`](manual-pod-fulfillment.md)); re-enable later; the reconciliation sweep picks up pending records.
5. Duplicate order at the provider → cancel the duplicate at the provider within its window; record in the fulfilment record notes.

Irreversible: provider cancellations after the window.

## Verification

- Record is `submitted` with a single external id; webhook moves it to `in_production` within the expected time.

## Prevention / follow-up

- Contract test for each new provider error code observed.
- Keep the manual gate enabled until two weeks of clean automated submissions.
