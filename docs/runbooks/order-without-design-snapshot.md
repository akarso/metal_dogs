# Runbook: order with a design line but no `OrderDesignSnapshot`

- **Introduced by slice:** `E4a`
- **Last exercised:** never
- **Severity when triggered:** high (a paid order cannot be fulfilled correctly)
- **Owner:** on-call operator

## Symptom

The signal is the log line `metaldogs.reconcile_snapshots.gap` from scheduler job `integration.sync.metaldogs.reconcile_snapshots`. `reason` is `missing_snapshot`, or `snapshot_job_failed` (the text is `snapshot_job_failed:` plus that job's last error). An alert on that line is not configured yet.

Until E4b binds an `OrderSource`, that line is not emitted. The flag defaults to false, so the cron is not registered. With the flag on and no source bound, the cron fails every run, logs `metaldogs.reconcile_snapshots.failed`, and emits no gap line. `./app metaldogs:snapshots reconcile` (with or without `--order`) exits `order source is not bound` and prints nothing (see Remediation).

The cron, once a source is bound, only lists orders created in the last 30 days. `snapshot_job_pending` means the snapshot job is still pending, processing, or reclaiming. `inspect_failed` means that order could not be read; other orders are still listed.

- Operator cannot find artwork for an order in admin (`E5`).

## Impact

Fulfilment is blocked for that order. The approved artwork still exists as a `DesignVersion` unless retention has deleted it — which is why the snapshot and the retention "keep" switch must happen promptly.

## Diagnosis

1. Identify the order id and the line key `(order_id, variant_id)` from `metaldogs.reconcile_snapshots.gap` (`order_id`, `line_key`, `reason`).
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

1. Job failed or never enqueued, design version intact → report, then repair. The report prints `order_id`, `line_key` (`order_id/variant_id`), and `reason`. It does not insert a snapshot and does not set production fields.

   - E4a, no `OrderSource` bound. Exits `metaldogs:snapshots reconcile: metaldogs reconcile: order source is not bound` and prints nothing:

```bash
./app metaldogs:snapshots reconcile
```

   - `--order` is the same exit and still prints nothing:

```bash
./app metaldogs:snapshots reconcile --order=<order_id>
```

   - `--fix` while `plugins.metaldogs.order_snapshot.enabled` is false is refused (`metaldogs:snapshots reconcile: --fix refused while plugins.metaldogs.order_snapshot.enabled is false`), including when no source is bound:

```bash
./app metaldogs:snapshots reconcile --fix
```

   - Report, only after a source is bound. Orders created in the last 30 days:

```bash
./app metaldogs:snapshots reconcile
```

   - `--order` inspects that id even when it is older than 30 days and does not scan the rest:

```bash
./app metaldogs:snapshots reconcile --order=<order_id>
```

   - `--fix` (flag must be true) enqueues `snapshot_order_design` (one job id per order; repeating `--fix` is safe) and skips lines whose reason starts with `inspect_failed`:

```bash
./app metaldogs:snapshots reconcile --order=<order_id> --fix
```

   - Cron `integration.sync.metaldogs.reconcile_snapshots` (`@every 5m`) is registered only when the flag is on. In E4a it fails every run (`order source is not bound`), logs `metaldogs.reconcile_snapshots.failed`, and emits no `metaldogs.reconcile_snapshots.gap` line. With a source bound it logs that gap line, does not insert snapshots or enqueue jobs, and sets production asset and checksum once if they are still NULL and the version is `ready`. `inspect_failed` on one order does not drop the other lines. There is no separate cron command; the row is this type:

```bash
./app jobs:list --type=integration.sync.metaldogs.reconcile_snapshots --limit=20
```
2. Job failing repeatedly → read the error; typical causes are a missing variant SKU mapping or a design in an unexpected status. Fix the cause, re-run.
3. Design version missing because the customer's design was never approved but the line passed validation → bug in the cart hook/checkout step; escalate, add regression test, and contact the customer to re-approve or refund through Shopanda.
4. Assets deleted by retention → restore from backup if within window; otherwise contact customer and refund.

Irreversible: refunds and customer communication. Never insert snapshot rows by hand. See "What a terminal-failed job means".

## Verification

- Snapshot exists for every design line of the order; design `status = ordered`; `E5` admin view shows the artwork with matching checksum.
- If production artwork became ready after the snapshot, `production_asset_id` and `production_checksum` are set. A second fill does not change them.

## Prevention / follow-up

- Keep the reconciliation sweep interval short (minutes). An alert on `metaldogs.reconcile_snapshots.gap` is not configured. When one is added, page on `missing_snapshot` and `snapshot_job_failed`. Do not page on `snapshot_job_pending`. Orders older than 30 days are outside the cron; pass `--order`.
- Retention must check snapshots **and** `approved` designs younger than the order grace window.
