# Runbooks

Operational procedures for an on-call operator who did not write the code. Each runbook follows [`TEMPLATE.md`](TEMPLATE.md) and is created or updated by the slice that introduces the failure mode (see "Docs" lines in [`../ROADMAP.md`](../ROADMAP.md)). Every runbook is exercised during the `O5` game day and carries a "last exercised" date afterwards.

Commands shown are Shopanda CLI commands from the upstream README and must be re-verified against the pinned tag in `F0`. Commands that do not exist yet are marked `TBD (slice)` rather than guessed.

| Runbook | Trigger | Introduced by |
| --- | --- | --- |
| [`local-development.md`](local-development.md) | Setting up or resetting a dev environment | F0, F2 |
| [`shopanda-upgrade.md`](shopanda-upgrade.md) | Merging a new upstream Shopanda tag | F0 |
| [`generation-job-stuck-or-failed.md`](generation-job-stuck-or-failed.md) | Jobs not progressing, failure-rate alert, customer stuck on progress screen | C4, G5 |
| [`ai-provider-outage-and-cost-cap.md`](ai-provider-outage-and-cost-cap.md) | Provider errors, rate limits, cost cap reached | C4, O3 |
| [`template-publishing.md`](template-publishing.md) | Adding, changing, publishing, or retiring templates | G2, X1 |
| [`order-without-design-snapshot.md`](order-without-design-snapshot.md) | Reconciliation alert; paid order with design line but no snapshot | E4 |
| [`manual-pod-fulfillment.md`](manual-pod-fulfillment.md) | Fulfilling an order by hand (M3 default; automation fallback) | P1 |
| [`pod-submission-failed.md`](pod-submission-failed.md) | Automated submission failed or stale | P2, P4 |
| [`asset-retention-and-deletion.md`](asset-retention-and-deletion.md) | Retention sweep issues; GDPR deletion request | O2 |
| [`backup-and-restore.md`](backup-and-restore.md) | Restore rehearsal or real recovery | O6 |

## Common context for all runbooks

- Processes: `app serve` (HTTP), `app worker` (jobs), `app scheduler` (cron). In dev, `app dev` runs all three.
- Correlation: every HTTP request and job log line carries `correlation_id`; design-scoped lines carry `design_id`; job lines carry `job_id`. Search logs by these ids, never by customer data.
- Feature flags (`plugins.metaldogs.*`): `enabled`, `customizer_public`, `generation.provider`, `generation.max_attempts_per_design`, `generation.max_retries`, `generation.cooldown_seconds`, `cost.daily_cap`, `fulfillment.auto_submit`, `fulfillment.manual_gate`, `retention.dry_run`. Exact keys are confirmed as slices land.
- Never paste signed URLs, tokens, provider payloads, or photos into tickets or chat.
