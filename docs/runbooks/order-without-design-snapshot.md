# Runbook: order with a design line but no `OrderDesignSnapshot`

- **Introduced by slice:** `E4a`
- **Last exercised:** never
- **Severity when triggered:** high (a paid order cannot be fulfilled correctly)
- **Owner:** on-call operator

## Symptom

- Alert from the `integration.sync.metaldogs.reconcile_snapshots` scheduler task: a design line with no snapshot whose `reason` is `missing_snapshot` or `snapshot_job_failed`. The cron only lists orders created in the last 30 days. `snapshot_job_pending` means the snapshot job is still pending, processing, or reclaiming — not a missing snapshot. `inspect_failed` means that order could not be read; other orders are still listed.
- Operator cannot find artwork for an order in admin (`E5`).

## Impact

Fulfilment is blocked for that order. The approved artwork still exists as a `DesignVersion` unless retention has deleted it — which is why the snapshot and the retention "keep" switch must happen promptly.

## Diagnosis

1. Identify the order id and the line key `(order_id, variant_id)` from the alert.
2. Check whether the handler ran. Search logs by `order_id` for `metaldogs.snapshot_order_design.written`, `.duplicate`, or `.failed`. List the jobs:

```bash
./app jobs:list --type=integration.sync.metaldogs.snapshot_order_design --limit=50
./app jobs:list --type=integration.sync.metaldogs.dispatch_snapshot_order --limit=50
./app jobs:list --type=integration.sync.metaldogs.snapshot_order_design --status=failed --limit=50
```

3. Check the design: does `metaldogs_designs.approved_version_id` exist for the `design_id` on the line? Is the design `approved` or `ordered`?
4. If the design version is missing or its assets are deleted, this is a data-loss incident: escalate and check [`backup-and-restore.md`](backup-and-restore.md).

## What a terminal-failed job means

`./app jobs:list --type=integration.sync.metaldogs.snapshot_order_design --status=failed` shows a job the queue has finished retrying. The reconcile reason is `snapshot_job_failed:` plus that job's last error.

The handler writes nothing for that order: no row in `metaldogs_order_design_snapshots`, and the Shopanda order is left as it was. The design stays `approved` when the failure is a missing design, a missing approved version, a missing pinned template, or a commit that rolled back. A flag that is on with no `OrderSource` bound fails the same way (`order source is not bound`). The default is that the flag is off, so these jobs are not registered and a real order has no snapshot until E4b.

Do not `INSERT` or `UPDATE` `metaldogs_order_design_snapshots` by hand. The table rejects deletes and rejects updates outside the two production columns, and a hand-written row would skip the `approved → ordered` transition. Repair is `--fix`, which only enqueues the job, after the cause is fixed and the flag is on.

## Remediation

1. Job failed or never enqueued, design version intact → report, then repair. `./app metaldogs:snapshots reconcile` reports orders created in the last 30 days. `./app metaldogs:snapshots reconcile --order=<order_id>` inspects that id even when it is older, and does not scan the rest. The report prints `order_id`, `line_key` (`order_id/variant_id`), and `reason`. It does not insert a snapshot and does not set production fields. `./app metaldogs:snapshots reconcile --order=<order_id> --fix` enqueues `snapshot_order_design` (one job id per order; repeating `--fix` is safe) and skips lines whose reason starts with `inspect_failed`. With `plugins.metaldogs.order_snapshot.enabled` false the report still runs and `--fix` is refused. The scheduler job `integration.sync.metaldogs.reconcile_snapshots` logs the same report and does not insert snapshots or enqueue jobs. When the flag is on, that cron also sets production asset and checksum once if they are still NULL and the version is `ready`. A read error on one order is `inspect_failed` and does not drop the other lines.
2. Job failing repeatedly → read the error; typical causes are a missing variant SKU mapping or a design in an unexpected status. Fix the cause, re-run.
3. Design version missing because the customer's design was never approved but the line passed validation → bug in the cart hook/checkout step; escalate, add regression test, and contact the customer to re-approve or refund through Shopanda.
4. Assets deleted by retention → restore from backup if within window; otherwise contact customer and refund.

Irreversible: refunds and customer communication. Never insert snapshot rows by hand. See "What a terminal-failed job means".

## Verification

- Snapshot exists for every design line of the order; design `status = ordered`; `E5` admin view shows the artwork with matching checksum.
- If production artwork became ready after the snapshot, `production_asset_id` and `production_checksum` are set. A second fill does not change them.

## Prevention / follow-up

- Keep the reconciliation sweep interval short (minutes). Alert on `missing_snapshot` and `snapshot_job_failed`. Do not page on `snapshot_job_pending`. Orders older than 30 days are outside the cron; pass `--order`.
- Retention must check snapshots **and** `approved` designs younger than the order grace window.
