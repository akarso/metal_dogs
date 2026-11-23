# CR-33: Shopanda pin `e062d4d3` (UPG11)

- Opened: 2026-11-23
- Pin: `e062d4d3…` (shopanda#658, PR-1602; includes shopanda#657 / PR-1601 and shopanda#656 / PR-1600)
- Previous published pin: `c25f3e7e…` (shopanda#653, PR-1599)
- `upstream/dev` tip at fetch: `e062d4d3` (this pin).
- `upstream/main` tip at fetch: `740f0b20` (shopanda#659). This pin is an ancestor of that tip. The three trees are equal (`d32e13ce`).

Merge-base of this branch and the previous pin is `c25f3e7e`.

## Ticket 00 — pin `e062d4d3`

- Date: 2026-11-23

### Where the pin sits

`e062d4d3` is the tip of `upstream/dev` and an ancestor of `upstream/main`. It is on both. It is not only on `dev`.

### Docs link guard

`PREVIOUS_PIN_FULL_SHA` in `.github/scripts/check-product-docs-links.sh` is the previous published pin on `origin/main` (`c25f3e7e…`). The full SHA is only in that script: this file is in the scan set.

### Conflict resolution

| File | Kept |
| --- | --- |
| `AGENTS.md` | Metal Dogs (`merge=ours`). Upstream added three plugin-recipe rows (`WithEventJobID`, `OrderPort` lines, `order.created` after the snapshot). That table is not in the product file. Build and test commands did not change. |
| `PLUGINS.md` | Metal Dogs (`merge=ours`). Upstream added one `WithEventJobID` clause to the sync-job row. The product file does not have that clause. |
| guides DEVELOPER.md | Deleted (modify/delete). Stub `docs/guides/README.md` stays |
| guides SYNC_JOB_OBSERVABILITY.md | Deleted (modify/delete). Read it at the pin on GitHub |
| `docs/phase-17-product-packaging/ROADMAP.md` | Deleted (modify/delete) |
| `docs/phase-17-product-packaging/prs/PR-1600.md` | Deleted (new upstream PR note) |
| `docs/phase-17-product-packaging/prs/PR-1601.md` | Deleted (new upstream PR note) |
| `docs/phase-17-product-packaging/prs/PR-1602.md` | Deleted (new upstream PR note) |
| `docs/phase-17-product-packaging/prs/PR-1602-CR.md` | Deleted (new upstream PR note) |
| `plugins/example/README.md` | Upstream payload sentence. The guide href stays a pin blob (existing deviation row) |

No conflict under `internal/`, `pkg/`, or `cmd/api`. Those paths match the pin except the allowlisted product files `cmd/api/register_products.go` and `cmd/api/register_plugins_metaldogs_test.go`.

`.env.example`, `configs/config.example.yaml`, `Makefile`, `docker-compose.yml`, `scripts/ci/docs-policy.sh` and its seed copy, `scripts/product-init/reserved-slugs.txt`, `scripts/product-init/product-init_test.sh`, and `.github/scripts/run-integration-tests.sh` are unchanged in this range and match the pin. Product lines stay in the companion files. Nothing was copied into those companions. No new deviation row.

`.gitignore` matches the pin outside the marker section.

`themes/default` is unchanged in this range. The product does not override those templates.

`plugins/metaldogs` is unchanged. It does not call `Lines`, `LinesForOrders`, `ListCreatedSince`, or `WithEventJobID`.

### Allowlist after this bump

Same sixteen paths as [CR-31](CR-31-upg10-pin-c25f3e7e.md). No path was added.

### Core migration 112

`migrations/112_orders_created_at_id_asc_index.sql` adds `idx_orders_created_at_id_asc` on `orders (created_at ASC, id ASC)` for `ListCreatedSince`. Product migrations are `plugins/metaldogs/migrations/001`–`015`. There is no product file `112_*.sql`. Plugin versions are `plugin:metaldogs:<basename>`, not the core sequence. No collision.

### Upstream range `c25f3e7e..e062d4d3`

`31e63bd7` (order lines and `ListCreatedSince`), `08b9bd66` (publish after snapshot), `211b2a3d` (`WithEventJobID`), `9d8371c6` (docs note), `63526afe` (CR harden), `f09acf31` (publish resume), `3fc42549` (retry `create_order` once), `9429cadf` (merge shopanda#656), `5298259a` (merge shopanda#657), `e062d4d3` (merge shopanda#658).

- `pkg/pluginsdk.OrderPort`: `Lines`, `LinesForOrders`, `ListCreatedSince`.
- `internal/application/checkout`: `finishCreatedOrder` snapshots, then publishes. Snapshot failure still publishes. `Workflow.Execute` retries `create_order` once.
- `pluginsdk.WithEventJobID` on `RegisterOnEvent`.
- `cmd/api/order_port_extensions.go` attaches the value reader for serve and worker.
- No change in this range to `plugin.App` registration, `plugin.Migrator`, cart, media storage, or the shipment updater.

E4b is not done.
