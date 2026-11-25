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
| [`checkout-manual-verification.md`](checkout-manual-verification.md) | Maintainer checklist before treating guest SSR checkout and Stripe test mode as done | E3 |
| [`order-without-design-snapshot.md`](order-without-design-snapshot.md) | Log `metaldogs.reconcile_snapshots.gap` (`missing_snapshot`, `snapshot_job_failed`); no alert configured. Flag off emits nothing. Flag on with `hostorders` bound: check both job types and the snapshot row. Last exercised: never | E4a / E4b |
| [`manual-pod-fulfillment.md`](manual-pod-fulfillment.md) | Fulfilling an order by hand (M3 default; automation fallback). Artwork fetch (route, CLI, permission, 300s expiry, `metaldogs_artwork_access_log`) is E5, [CR-35](../cr-rounds/CR-35-e5-artwork-access.md). Listing and recording a POD id stay `TBD (P1)` | P1, E5 |
| [`pod-submission-failed.md`](pod-submission-failed.md) | Automated submission failed or stale | P2, P4 |
| [`asset-retention-and-deletion.md`](asset-retention-and-deletion.md) | Retention sweep issues; GDPR deletion request | O2 |
| [`backup-and-restore.md`](backup-and-restore.md) | Restore rehearsal or real recovery | O6 |

## Common context for all runbooks

- Processes: `app serve` (HTTP), `app worker` (jobs), `app scheduler` (cron). In dev, `app dev` runs all three.
- Correlation: every HTTP request and job log line carries `correlation_id`; design-scoped lines carry `design_id`; job lines carry `job_id`. Search logs by these ids, never by customer data.
- Feature flags (`plugins.metaldogs.*`): `enabled`, `customizer_public`, `generation.provider`, `generation.max_attempts_per_design`, `generation.max_retries`, `generation.cooldown_seconds`, `order_snapshot.enabled` (default false; snapshot jobs E4a/E4b, [CR-32](../cr-rounds/CR-32-e4a-order-design-snapshot.md) / [CR-34](../cr-rounds/CR-34-e4b-order-source.md); artwork route, `metaldogs.snapshots.read`, and `metaldogs:order-artwork` E5, [CR-35](../cr-rounds/CR-35-e5-artwork-access.md); order-thumbnail route only when this flag is true and `thumbnail_signing_key` is set, E6a, [CR-37](../cr-rounds/CR-37-e6a-order-thumbnail.md)), `thumbnail_signing_key` (no default; at least 32 characters; shorter fails Init; absent leaves the thumbnail route unregistered), `public_base_url` (no default; absolute http(s) origin for `OrderThumbnailURL`; the order-line image hook registers only when this origin is https, E6b, [CR-42](../cr-rounds/CR-42-e6b-order-thumbnail-display.md); `http://localhost` logs a warning and does not register; the platform also drops image URLs whose host is not public, so a local origin does not show the picture), `production_spec` (omit the section unless configured; no defaults; absent keeps `prepare_production_artwork` on `pending_spec`; G5a, [CR-36](../cr-rounds/CR-36-g5a-production-artwork.md)), `cost.daily_cap`, `fulfillment.auto_submit`, `fulfillment.manual_gate`, `retention.dry_run`. Exact keys are confirmed as slices land.
- Never paste signed URLs, tokens, provider payloads, or photos into tickets or chat.
