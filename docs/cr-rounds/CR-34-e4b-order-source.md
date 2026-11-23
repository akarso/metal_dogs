# CR-34: E4b order source adapter

- Status: **closed**
- Closed: 2026-11-23
- Slices: `E4b`
- Milestone progress: M3 → E4b

## What shipped

Real order source for the E4a snapshot path. Dark. The flag stays false.

- `plugins/metaldogs/adapters/hostorders` implements `ports.OrderSource` over `Host.Orders()`. `Lines` and `LinesForOrders` map `OrderLineView.Extensions["metaldogs.design_id"]`. Absent, null, or blank is no design. A non-string value is an error. An order the port omits has no lines.
- `Plugin.Init` binds that source only when `plugins.metaldogs.order_snapshot.enabled` is true. It calls `Require(CapOrders)`. A root that asserts capabilities then fails boot if the orders port is unbound (no skip list omits `CapOrders`) (`required capability "orders" not bound`). A root that does not assert capabilities leaves the source nil; the snapshot job and the sweep stay fail-closed (`order source is not bound`).
- `dispatch_snapshot_order` uses `pluginsdk.WithEventJobID`: UUID v5 of the dispatcher job type and the order id. That id is not the snapshot job id. A second `order.created` is the same dispatcher job.
- An invalid event job id never becomes a row. `enqueueSyncJob` passes the callback result to `plugin.NewSyncJobEnvelopeWithID` (`internal/application/integration/sync_runtime.go`, `internal/platform/plugin/sync_job.go`). A blank id returns `job id requires a non-empty UUID` and does not enqueue. A non-empty value that is not a UUID returns `job id %q is not a UUID` and does not enqueue. This callback returns `""` only for a blank order id; any other order id is hashed, so the non-UUID rejection is the SDK path, not this callback. The snapshot job uses `WithJobID` and rejects a non-UUID order id the same way.
- `reconcile_snapshots` and the CLI page `ListCreatedSince` at 100, cursor = last id, same `since`, then one `LinesForOrders` per page. `--order` still calls `Lines` and does not list.
- No edits under `internal/*`, `pkg/*`, or core `cmd/api/*`.

## Verification results

| Item | Result |
| --- | --- |
| `TestCheckout_OrderSnapshot` | In the tree. Isolated Postgres, `POST /api/v1/checkout`, then `RegisterSyncJobHandlers` on the queued dispatcher and snapshot jobs. Not skipped. |
| `bash .github/scripts/check-product-docs-links.sh` | Exit 0 on 2026-11-23 in this worktree |

## Tests added

- `plugins/metaldogs/checkout_snapshot_test.go` — `TestCheckout_OrderSnapshot`: flag off enqueues nothing; one custom tee copies every snapshot field and sets the design `ordered`; two sizes write two snapshots; a second `order.created` keeps one dispatcher job and one snapshot; another product writes no snapshot
- `plugins/metaldogs/adapters/hostorders/source_test.go` — design id mapping, non-string error, list order, absent port is nil
- `plugins/metaldogs/adapters/memory/order_source_test.go` — keyset order, `afterID` outside the window, limit 1..100
- `plugins/metaldogs/application/reconcile_snapshots_test.go` — `TestReconcileRun_PagesEachOrderOnce`
- `plugins/metaldogs/snapshot_order_test.go` — flag on binds `Host.Orders()`; flag off does not

## Flags / migrations / rollback

- `plugins.metaldogs.order_snapshot.enabled` stays default false (`configs/config.example.product.yaml`). Env `SHOPANDA_PLUGINS_METALDOGS_ORDER_SNAPSHOT_ENABLED`.
- No new migration. Rollback: leave the flag false. Migration `015` stays.
- Logs stay order id, variant id, design id. No customer data, no image bytes, no signed URLs.

## Documentation updated

- [x] `docs/ROADMAP.md`: E4b **DONE** (CR-34). §1 rows for the extension copy and `order.created` name the adapter and the default-off flag. E4a known limits point at CR-34.
- [x] `docs/architecture/flows/checkout-snapshot-and-fulfillment.md`: pin publishes `order.created` after the extension copy; `hostorders` reads `metaldogs.design_id`. Absent design id is skipped; a non-string fails.
- [x] `docs/architecture/domain-model.md`, `docs/architecture/flows/design-to-order.md`, `docs/architecture/overview.md`, `docs/UPSTREAM_PIN.md`: same status.
- [x] ADR-0005 status: E4b binds `Host.Orders()` when the flag is on. The flag still defaults to false. `docs/adr/README.md` matches.
- [x] `docs/runbooks/order-without-design-snapshot.md`: checks before the flag is turned on (pin `e062d4d3`, `Host.Orders()`, migration `015`), then both job types, the written log, the snapshot row, and reconcile. An unbound orders port fails boot on a root that asserts capabilities (no skip list omits `CapOrders`). A root that does not assert capabilities still exits `order source is not bound`. Last exercised: never.
- [x] `docs/runbooks/asset-retention-and-deletion.md`: unchanged. The flag stays false, so no snapshot rows are written and the retention skip stays idle.
- [x] `configs/config.example.product.yaml` and `plugins/metaldogs/README.md`: flag on binds `hostorders`.
- [x] Index row in this file's parent README. CR-32 and CR-33 index rows point here.

## Follow-ups

- M3 turns `plugins.metaldogs.order_snapshot.enabled` on. This round does not.
- A missing order and an order with no design lines both end the snapshot job `done`. The port returns no lines for a missing id.
- G5: call `FillSnapshotProduction` when a version becomes `ready`.
- O5: exercise the runbook and replace "Last exercised: never".
- E6 and track P stay out of this slice. E5 artwork access closed in [CR-35](CR-35-e5-artwork-access.md).
