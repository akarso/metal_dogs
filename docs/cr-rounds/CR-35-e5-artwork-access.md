# CR-35: E5 admin snapshot and artwork access

- Status: **closed**
- Closed: 2026-11-23
- Slices: `E5`
- Milestone progress: M3 → E5

## What shipped

Operator fetch of snapshot artwork. Dark. `plugins.metaldogs.order_snapshot.enabled` stays false.

- `application.OrderArtworkAccess.Access` is the only service behind the admin route and the CLI. It loads snapshot lines, signs `artwork_asset_id` through `AssetStore.SignedGetURL` and, when set, `production_asset_id` through `DispositionSigner.SignedGetURLDisposition` with attachment. TTL is `OrderArtworkTTL` (300s), not caller-controlled, still passed through `CapSignedGetTTL` (15m cap). `production` is null when the production asset id is empty. Approved artwork keeps the mediastore rule (inline for `.jpg` / `.jpeg` / `.png` / `.webp`). Production is an attachment even when the key is one of those extensions.
- Order existence is `ports.OrderLookup.Exists`, which calls `Host.Orders().FindByID` (`adapters/hostorders`). Unknown order is not found. An order with no snapshot rows is an empty `lines` list. A non-UUID id is `ports.ErrInvalidOrderID`.
- Migration `016` creates `metaldogs_artwork_access_log`. One row per issued URL: `order_id`, `variant_id`, `kind` (`approved` or `production`), `actor`, `channel` (`admin` or `cli`), `requested_at`. Append-only trigger refuses update and delete. The row has no URL and no storage key. `Append` runs before the URLs are returned. A failed write returns `ErrArtworkAccessFailed` and no URL.
- Structured log `metaldogs.order_artwork.access` carries `order_id` and `actor` only. A non-UUID order id is info. Error is reserved for `ErrArtworkAccessFailed`.
- Permission `metaldogs.snapshots.read` is `Host.RegisterPermission` for role `admin` only. Route `GET /api/v1/admin/orders/{order_id}/artwork` is `Host.RegisterAdminRoute` with that permission. CLI `metaldogs:order-artwork <order_id>` prints the same JSON. The shell does not check the permission. Actor is the literal `cli`, channel `cli`.
- HTTP actor is `pluginsdk.CustomerID(r)`, which is `auth.StorefrontActorID`: the request identity's user id, empty for a guest. `admin.RequirePermission` returns 401 for a guest before the handler, so a 200 stores that user id. On `/api/v1/admin/*`, `storefront.AuthMiddleware` keeps a staff Bearer and does not replace it with the storefront session cookie (`auth_middleware.go`).
- Route, permission, and CLI register only inside the `order_snapshot.enabled` branch of `Plugin.Init` (same gate as the snapshot jobs). Flag off: they are absent. The CLI handler, if called, exits non-zero with `plugins.metaldogs.order_snapshot.enabled is false`.
- No edits under `internal/*`, `pkg/*`, or core `cmd/api/*`.

## Decisions

- Audit stays in the plugin table. Plugins have no audit port. That gap is already a non-blocking Shopanda roadmap row. This round does not re-open it and does not add a platform call.
- Actor for HTTP is the SDK principal `pluginsdk.CustomerID`, not a new admin-user helper. `pkg/pluginsdk/httpadmin.go` does not expose a separate admin user id. `CustomerID` reads the same request identity `RequirePermission` already checked, and on this admin path that identity is the staff account.
- TTL is fixed in the service. Callers cannot pass a longer life. 300s is under `MaxSignedGetTTL`, so the cap does not shorten it.
- Empty snapshot and unknown order stay distinct: `200` with `lines: []` versus `404` `plugin.metaldogs.order_not_found`.
- CLI trust boundary is the shell. The call is still audited.

## How the test proves expiry

`TestOrderArtworkAdminRoute` and `TestOrderArtworkCLI` share `expiringStore` and `testutil.FakeClock`. `SignedGetURL` records `exp = clock.Now() + ttl` and returns `https://artwork.invalid/uN`. `Fetch` returns the bytes only while `clock.Now().Before(exp)`. The tests fetch both URLs at `start`, then `clock.Advance(OrderArtworkTTL + time.Second)`, then `Fetch` returns an error (`expired`). The HTTP test mounts the real `admin.RequirePermission` and `storefront.AuthMiddleware` on the registered route. The body is checked for the storage key and `private/blobs` and must not contain them. Audit rows are read from `metaldogs_artwork_access_log` and must not contain a URL.

## Verification results

| Item | Result |
| --- | --- |
| `TestOrderArtworkAdminRoute` | In the tree. Isolated Postgres. Not skipped |
| `TestOrderArtworkCLI` | In the tree. Same service, actor `cli` |
| `bash product/checks/metaldogs-docs.sh` | Exit 0 on 2026-11-23 in this worktree (`check-product-docs-links.sh` and `check-upstream-deviations.sh`; pin `e062d4d3d479`, 16 listed paths) |

## Tests added

- `plugins/metaldogs/order_artwork_http_test.go` — `TestOrderArtworkAdmin_RegisteredOnlyWhenFlagOn`: flag off registers neither the route nor the permission. `TestOrderArtworkAdminRoute`: `401` `unauthorized` with no login; `403` for a manager; `200` for admin `admin-42` with metadata and both URLs; audit rows for `approved` and `production` with that actor and channel `admin`; `production: null` writes one `approved` row; unknown order `404` and no row; empty lines `200` and no row; bad id `400` `plugin.metaldogs.invalid_order_id`; renamed audit table → `503` `plugin.metaldogs.artwork_unavailable`, no URL, no new row
- `plugins/metaldogs/order_artwork_test.go` — `TestOrderArtworkCommand_RegisteredOnlyWhenFlagOn`; `TestOrderArtworkCLI`: same data, actor `cli`, flag off refuses, append-only update/delete refused
- `plugins/metaldogs/adapters/postgres/artwork_access_log_test.go` — `TestArtworkAccessLogRepo_BadEventWritesNothing`

## Flags / migrations / rollback

- `plugins.metaldogs.order_snapshot.enabled` stays default false (`configs/config.example.product.yaml`). Env `SHOPANDA_PLUGINS_METALDOGS_ORDER_SNAPSHOT_ENABLED`.
- Migration `016` `metaldogs_artwork_access_log`. Rollback of behaviour: leave the flag false. The table stays.
- Logs: order id and actor. No URL, no storage key, no image bytes.

## Documentation updated

- [x] `docs/ROADMAP.md`: E5 **DONE** (CR-35). E4a/E4b known limits point here. G5 no longer says failures are visible in an E5 admin screen.
- [x] `docs/architecture/api.md`: route, codes, TTL, audit columns, actor.
- [x] ADR-0004 status: E5 landed. `docs/adr/README.md` matches.
- [x] `docs/runbooks/manual-pod-fulfillment.md`: route and CLI, permission, 300s expiry, audit table. P1 list/record commands stay `TBD (P1)`. Last exercised: never.
- [x] `docs/runbooks/README.md`: flag note and the runbook row.
- [x] `docs/runbooks/order-without-design-snapshot.md`: empty lines versus not found; checksum check is the CLI.
- [x] `docs/architecture/domain-model.md`, `docs/architecture/flows/checkout-snapshot-and-fulfillment.md`, `docs/architecture/overview.md`: the log and the flag gate.
- [x] `configs/config.example.product.yaml` and `plugins/metaldogs/README.md`: flag on registers the artwork surfaces.
- [x] Index row in this file's parent README. CR-32 and CR-34 follow-ups point here.
- [x] `AGENTS.md`: `metaldogs:order-artwork` in the command list.

## Follow-ups

- M3 turns `plugins.metaldogs.order_snapshot.enabled` on. This round does not.
- E6: customer thumbnail. Out of this slice.
- P1: list pending fulfilment and record an external POD id. The artwork download this runbook calls is done.
- G5: fill production columns. Until then `production` is null.
- O5: exercise the runbook and replace "Last exercised: never".
