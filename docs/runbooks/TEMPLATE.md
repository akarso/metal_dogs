# Runbook: <title>

- **Introduced by slice:** `XN`
- **Last exercised:** never | YYYY-MM-DD (game day / incident)
- **Severity when triggered:** low | medium | high
- **Owner:** role

## Symptom

How the problem shows up: alert name, dashboard signal, customer report, log pattern.

## Impact

Who is affected and what is at risk (customer flow, money, data).

## Diagnosis

Ordered steps, each with the exact command or query and what a healthy vs unhealthy result looks like. Use ids, never personal data.

## Remediation

Ordered steps. Mark irreversible steps clearly. Prefer flag changes and job retries over data edits. Never edit production data by hand when a command exists; if none exists, file the slice that adds it.

## Verification

How to confirm the system is healthy again.

## Prevention / follow-up

Alert tuning, test to add, slice to schedule.
