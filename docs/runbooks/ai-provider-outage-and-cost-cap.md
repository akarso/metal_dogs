# Runbook: AI provider outage, rate limits, and cost cap

- **Introduced by slice:** `C4`, `O3`
- **Last exercised:** never
- **Severity when triggered:** high (customer flow blocked or money at risk)
- **Owner:** on-call operator

## Symptom

- Alerts: provider error rate, provider latency p95, `rate_limited` count, `cost_cap_reached` events, daily spend approaching cap.
- Jobs failing with `provider_timeout`, `provider_error`, `provider_rate_limited`, or `cost_cap_reached`.
- Customers see "temporarily unavailable" on generate.

## Impact

No new previews; designs are preserved and resumable. Risk of runaway spend if retries are misconfigured.

## Diagnosis

1. Confirm scope: failures across all templates and photos → provider; one template → see template runbook.
2. Check the provider's status page and the per-job `provider_request_id` for a sample in their dashboard.
3. Check spend: `TBD (O3)` command/dashboard showing today's attempts, success rate, cost, cost per accepted artwork.
4. Check the cap: `plugins.metaldogs.cost.daily_cap` vs recorded spend.
5. Check for a retry storm: number of attempts per job above `max_retries` would indicate a bug — stop the worker and escalate.

## Remediation

1. **Stop the bleeding** if spend is abnormal: set `plugins.metaldogs.generation.enabled: false` (or lower the cap to current spend). Generation endpoints then return `503 temporarily_unavailable`; everything else keeps working.
2. Provider incident, spend normal: leave the queue running; transient retries are bounded. If the incident is long, disable generation to avoid exhausting customers' attempt budgets with failures — failed attempts due to provider errors should not count against the customer (`C7` policy); verify this is implemented before relying on it.
3. Cap reached legitimately (good day): raise the cap deliberately after confirming the success rate and cost per accepted artwork are within budget. Record the change.
4. Credentials/auth error: rotate the key in the secret store and restart `app serve` and `app worker`. Never put the key in config files or logs.
5. After recovery: re-enable generation; failed jobs are not retried automatically (cost); customers retry themselves.

Irreversible: none. Raising the cap spends money; record who approved it.

## Verification

- Success rate back to baseline on the dashboard; a test design with the fake provider is not meaningful here — use one real generation on a staff design and confirm cost recorded once.

## Prevention / follow-up

- Keep `max_retries` small; alert on attempts-per-job anomalies.
- Review cost per accepted artwork weekly; feed into ADR-0008 revisits.
