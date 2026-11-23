# Runbook: order with a design line but no `OrderDesignSnapshot`

- **Introduced by slice:** `E4a`
- **Last exercised:** never
- **Severity when triggered:** high (a paid order cannot be fulfilled correctly)
- **Owner:** on-call operator

## Symptom

The signal is the log line `metaldogs.reconcile_snapshots.gap` from scheduler job `integration.sync.metaldogs.reconcile_snapshots`. `reason` is `missing_snapshot`, or `snapshot_job_failed` (the text is `snapshot_job_failed:` plus that job's last error). An alert on that line is not configured yet.

The flag defaults to false, so the cron is not registered and that line is not emitted. With the flag on, `Init` calls `Require(CapOrders)` and binds `adapters/hostorders` when `Host.Orders()` is present, and the cron emits the gap line for the last 30 days (see below). A root that asserts capabilities fails boot if that port is unbound (`required capability "orders" not bound`). A root that does not assert capabilities leaves the source nil: the cron fails the run, logs `metaldogs.reconcile_snapshots.failed`, and emits no gap line. `./app metaldogs:snapshots reconcile` with no source (with or without `--order`) exits `order source is not bound` and prints nothing (see Remediation).

The cron, once `hostorders` is bound, only lists orders created in the last 30 days. `snapshot_job_pending` means the snapshot job is still pending, processing, or reclaiming. `inspect_failed` means that order could not be read; other orders are still listed.

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

The handler writes nothing for that order: no row in `metaldogs_order_design_snapshots`, and the Shopanda order is left as it was. The design stays `approved` when the failure is a missing design, a missing approved version, a missing pinned template, or a commit that rolled back. On a root that asserts capabilities, a flag that is on with no orders port fails boot (`required capability "orders" not bound`) before a job is stored. A root that does not assert capabilities runs the job and fails it (`order source is not bound`). The default is that the flag is off, so these jobs are not registered and a real order has no snapshot. With the flag on and `Host.Orders()` present, a custom-tee line gets a snapshot (see below).

Do not `INSERT` or `UPDATE` `metaldogs_order_design_snapshots` by hand. The table rejects deletes and rejects updates outside the two production columns, and a hand-written row would skip the `approved → ordered` transition. Repair is `--fix`, which only enqueues the job, after the cause is fixed and the flag is on.

## Remediation

1. Job failed or never enqueued, design version intact → report, then repair. The report prints `order_id`, `line_key` (`order_id/variant_id`), and `reason`. It does not insert a snapshot and does not set production fields.

   - No `OrderSource` bound. That is the flag off, or a root that does not assert capabilities with the flag on and `Host.Orders()` absent. A root that asserts capabilities, with the flag on and that port unbound, does not reach this command: boot fails with `required capability "orders" not bound`. The unbound-source exit is `metaldogs:snapshots reconcile: metaldogs reconcile: order source is not bound` and it prints nothing:

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

   - Cron `integration.sync.metaldogs.reconcile_snapshots` (`@every 5m`) is registered only when the flag is on. A root that asserts capabilities fails boot if `Host.Orders()` is unbound. A root that does not assert capabilities fails the run (`order source is not bound`), logs `metaldogs.reconcile_snapshots.failed`, and emits no `metaldogs.reconcile_snapshots.gap` line. With `hostorders` bound it logs that gap line, does not insert snapshots or enqueue jobs, and sets production asset and checksum once if they are still NULL and the version is `ready`. `inspect_failed` on one order does not drop the other lines. A canceled `LinesForOrders` page returns that error and does not read each order. There is no separate cron command; the row is this type:

     ```bash
     ./app jobs:list --type=integration.sync.metaldogs.reconcile_snapshots --limit=20
     ```
2. Job failing repeatedly → read the error; typical causes are a missing variant SKU mapping or a design in an unexpected status. Fix the cause, re-run.
3. Design version missing because the customer's design was never approved but the line passed validation → bug in the cart hook/checkout step; escalate, add regression test, and contact the customer to re-approve or refund through Shopanda.
4. Assets deleted by retention → restore from backup if within window; otherwise contact customer and refund.

Irreversible: refunds and customer communication. Never insert snapshot rows by hand. See "What a terminal-failed job means".

## Before turning the flag on

M3 sets `plugins.metaldogs.order_snapshot.enabled`. This slice leaves it false. Check these first:

1. The pin is `e062d4d3` or later. `OrderPort` has `Lines`, `LinesForOrders`, and `ListCreatedSince`. `finishCreatedOrder` copies cart extensions, then publishes `order.created`. `pluginsdk.WithEventJobID` is present.
2. `Host.Orders()` is present. `Init` calls `Require(CapOrders)` and `hostorders.FromHost`. A root that asserts capabilities fails boot if that port is unbound (`required capability "orders" not bound`; no skip list omits `CapOrders`). A root that does not assert capabilities leaves the source nil and the snapshot job fails with `order source is not bound`.
3. Migration `015` is applied (`metaldogs_order_design_snapshots`).
4. Restart, then use the checks below. Do not insert snapshot rows by hand.

## With the flag on and the adapter bound

`plugins.metaldogs.order_snapshot.enabled` is true and `Host.Orders()` is present. `Init` requires `CapOrders` and sets `adapters/hostorders`.

1. A paid checkout copies `metaldogs.design_id` onto the order line, then publishes `order.created`. That enqueues one `integration.sync.metaldogs.dispatch_snapshot_order` (UUID v5 of that job type and the order id). The handler enqueues one `integration.sync.metaldogs.snapshot_order_design`.
2. Confirm one row of each type for the order. A second `order.created` does not add a dispatcher job.

```bash
./app jobs:list --type=integration.sync.metaldogs.dispatch_snapshot_order --limit=50
./app jobs:list --type=integration.sync.metaldogs.snapshot_order_design --limit=50
```

3. Logs: `metaldogs.snapshot_order_design.written` carries order id, variant id, and design id. A line with no design id is skipped. A `metaldogs.design_id` that is not a string fails the job (`metaldogs.snapshot_order_design.failed`).
4. `metaldogs_order_design_snapshots` has one row per custom-tee line and that design is `ordered`. A line for another product has no row.
5. `./app metaldogs:snapshots reconcile` prints gaps in the last 30 days. `--order=<id>` inspects one order. `--fix` enqueues the snapshot job and does not insert a row itself. The cron logs `metaldogs.reconcile_snapshots.gap` and does not enqueue. No alert is configured on that line.

## Verification

- Snapshot exists for every design line of the order; design `status = ordered`; `E5` admin view shows the artwork with matching checksum.
- If production artwork became ready after the snapshot, `production_asset_id` and `production_checksum` are set. A second fill does not change them.

## Prevention / follow-up

- Keep the reconciliation sweep interval short (minutes). An alert on `metaldogs.reconcile_snapshots.gap` is not configured. When one is added, page on `missing_snapshot` and `snapshot_job_failed`. Do not page on `snapshot_job_pending`. Orders older than 30 days are outside the cron; pass `--order`.
- Retention must check snapshots **and** `approved` designs younger than the order grace window.
