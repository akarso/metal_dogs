# CR-37: E6a order thumbnail link

- Status: **closed**
- Closed: 2026-11-24
- Slices: `E6a`
- Milestone progress: M3 → E6a

## What shipped

A stable public thumbnail for one order line. Dark. `plugins.metaldogs.order_snapshot.enabled` stays false. Email and storefront pages do not call the link (E6b).

- Migration `018` adds nullable `mockup_asset_id` and `thumbnail_asset_id` on `metaldogs_order_design_snapshots`. A check requires both null or both set. The append-only trigger treats both as insert-only. Production columns stay the only columns that may change later, once, from NULL.
- `snapshot_order_design` looks up the mockup of the generation job that stored the approved artwork, then the design's latest mockup with the same artwork checksum. It stores a PNG (longest edge ≤ 320 px, no metadata) as asset type `thumbnail` with a null `owner_ref` (one live row per `(owner_ref, type)` in migration `009`; two lines can share a design). The asset id is a UUID v5 of the order id and variant id. No mockup, a read/encode/store failure, or a thumbnail dropped at commit (insert conflict, or the mockup is no longer live): the snapshot is still written with both columns null. Each of those logs `metaldogs.snapshot_order_design.thumbnail` with `order_id` and `variant_id` only. The object written for that attempt is deleted unless a live asset row still has that storage key. A commit that rolls the order back deletes the same way. A lookup error leaves the object. An insert failure rolls back to a savepoint and does not abort the order.
- `AssetsReferencedBySnapshots` also matches `mockup_asset_id` and `thumbnail_asset_id`.
- `GET /api/customizer/order-thumbnails/{token}` is public. The token is `base64url(order_id|variant_id).base64url(HMAC-SHA256(key, "order-thumbnail:" + order_id + "|" + variant_id))`. It does not expire. Compare is `hmac.Equal`. The handler writes the bytes: allowlisted `Content-Type` (`image/jpeg`, `image/png`, `image/webp`), `Content-Disposition: inline`, `Cache-Control: private, max-age=86400` on `200`, `X-Content-Type-Options: nosniff`. The route is registered without `NoCache`. Bad token, unknown snapshot, no thumbnail, missing object, or a MIME outside the allowlist: `404` `{"error":"not_found"}` with `Cache-Control: no-store`. No storage key in headers or body. The handler does not log the token. The platform HTTP access log (and any proxy log) records the full request path, so the token appears there on every hit: treat those logs as credential-bearing for this route. Platform gap, not fixable in the plugin: the access log should use the route pattern or redact the path per route. A snapshot, asset, or object read error is still that `404` and logs `metaldogs.order_thumbnail` once, with `order_id`, `variant_id`, and `cause` (`snapshot`, `asset`, or `object`). The error text is not logged.
- `plugins.metaldogs.thumbnail_signing_key` has no default. Shorter than 32 characters fails Init; the value is not in the error. Absent: the route is not registered and `OrderThumbnailURL` returns empty, including when the snapshot flag is true.
- `(*Plugin).OrderThumbnailURL` returns `<public_base_url>/api/customizer/order-thumbnails/<token>` when the line has a thumbnail. `plugins.metaldogs.public_base_url` is the origin. `Host.ConfigValue` is plugin-scoped and `Host.PlatformConfig` does not allow `server.public_base_url`.
- No edits under `internal/*`, `pkg/*`, or core `cmd/api/*`.

## Decisions

- The thumbnail URL is the one exception to the 15-minute signed-URL rule ([ADR-0004](../adr/0004-private-object-storage-signed-urls.md)). The handler serves bytes. The image is small, the token is unguessable, and the response has no storage path.
- A missing key is not a boot error. The feature stays off until the operator sets one.
- The store origin is `plugins.metaldogs.public_base_url`. There is no existing plugin setting for it.
- When the generation job for the approved artwork is missing, the snapshot uses the design's latest mockup with the same artwork checksum.
- E6b stays open. Pin `084d1206` ([CR-38](CR-38-upg12-pin-084d1206.md)) puts `VariantID`, `SKU`, and public `order_item` extensions on the confirmation email and the storefront order pages. There is no enrich hook for a derived image URL. E6b is not done.

## Verification results

| Item | Result |
| --- | --- |
| `TestOrderThumbnailHTTP` | In the tree. Isolated Postgres, in-memory store. Not skipped |
| `TestSnapshotOrder_Thumbnail` | In the tree. Isolated Postgres |
| `TestRetention_MockupAndThumbnailSurviveEachPath` | In the tree. Isolated Postgres |
| `bash .github/scripts/check-product-docs-links.sh` | Exit 0 on 2026-11-24 in this worktree |
| `bash .github/scripts/check-upstream-deviations.sh` | Exit 0 on 2026-11-24: `upstream-deviations OK (pin e062d4d3d479, 16 listed path(s))` |

## Tests added

- `plugins/metaldogs/snapshot_order_test.go` — `TestSnapshotOrder_Thumbnail`: mockup sets both columns; longest edge ≤ 320 and no metadata; no mockup or a failed encode/store still writes the snapshot with null columns; rerun keeps one object; two sizes of one design get two thumbnail ids; a thumbnail id that already exists still commits the snapshot with null columns and logs the thumbnail warning; the new object is deleted when its key is not the live row's key, and kept when it is; a commit that rolls back deletes the thumbnail objects from that attempt
- `plugins/metaldogs/adapters/postgres/retention_test.go` — `TestRetention_MockupAndThumbnailSurviveEachPath`
- `plugins/metaldogs/application/order_thumbnail_test.go` — `TestOrderThumbnailBytes_FaultsStayOutOfTheMiss`: snapshot, asset, and object read errors are `OrderThumbnailFault` with order id and variant id; the error text has no storage key; a missing asset row stays `ErrOrderThumbnailMiss`
- `plugins/metaldogs/order_thumbnail_http_test.go` — `TestOrderThumbnailHTTP`: the URL from `OrderThumbnailURL` returns the image and the four headers; storage key absent from headers, body, and log; changed order id, changed variant id, random token, truncated token, no thumbnail, and missing object share one `404` body; the missing object logs `metaldogs.order_thumbnail` with order id, variant id, and cause `object`; empty link when the line has no thumbnail. `TestOrderThumbnail_NotRegisteredWithoutFlagOrKey`. `TestOrderThumbnail_ShortKeyIsSettingsError`
- `plugins/metaldogs/settings_test.go` — `TestSettingsFrom_ThumbnailKeyAndPublicBase`
- `plugins/metaldogs/thumbnail/token_test.go` — `TestToken_RoundTripAndTamper`
- `plugins/metaldogs/thumbnail/png_test.go` — `TestPNG_LongestEdgeAndNoAncillaryChunks`, `TestPNG_RejectsGarbage`

## Flags / migrations / rollback

- `plugins.metaldogs.order_snapshot.enabled` stays default false.
- `plugins.metaldogs.thumbnail_signing_key`: no default. Env `SHOPANDA_PLUGINS_METALDOGS_THUMBNAIL_SIGNING_KEY`.
- `plugins.metaldogs.public_base_url`: no default. Env `SHOPANDA_PLUGINS_METALDOGS_PUBLIC_BASE_URL`.
- Migration `018`. Rollback of behaviour: leave the flag false, or leave the key unset. The columns stay.
- Logs: order id and variant id on a thumbnail miss. No token, no storage key, no image bytes.

## Documentation updated

- [x] `docs/ROADMAP.md`: E6a **DONE** (CR-37). E6b open with the two Shopanda blockers. M3 lists E6a; M4 lists E6b. E4a/E4b/E5 known limits point here.
- [x] `docs/architecture/api.md`: token, headers, uniform `404`, `public_base_url`, no `NoCache`.
- [x] ADR-0004 amendment: the thumbnail endpoint is the exception to the 15-minute rule. `docs/adr/README.md` matches.
- [x] ADR-0005 amendment: insert stores `mockup_asset_id` and `thumbnail_asset_id`. `docs/adr/README.md` matches.
- [x] `docs/architecture/flows/checkout-snapshot-and-fulfillment.md`: snapshot-time thumbnail and the public endpoint.
- [x] `docs/architecture/domain-model.md`: both columns, retention check, policy row.
- [x] `docs/architecture/overview.md`: route registers only when the key is set.
- [x] `docs/runbooks/asset-retention-and-deletion.md`: mockup and thumbnail are snapshot-protected. Last exercised: never.
- [x] `docs/runbooks/README.md`: `thumbnail_signing_key` and `public_base_url`.
- [x] `configs/config.example.product.yaml` and `plugins/metaldogs/README.md`: key, base URL, route.
- [x] Index row in this file's parent README.
- [x] `AGENTS.md`: no new CLI command. Build and test commands unchanged.

## Follow-ups

- Shopanda gap: the HTTP access log writes the full request path, so non-expiring thumbnail tokens land in it. Needs route-pattern logging or per-route path redaction. Add to the Shopanda roadmap.

- M3 turns `plugins.metaldogs.order_snapshot.enabled` on and sets `thumbnail_signing_key` and `public_base_url`. This round does not.
- E6b: call `OrderThumbnailURL` from the confirmation email and order pages. Pin `084d1206` ([CR-38](CR-38-upg12-pin-084d1206.md)) already puts `VariantID`, `SKU`, and public `order_item` extensions on those lines. There is no enrich hook for a derived image URL. E6b is not done. This round does not edit those templates.
- O5: exercise the retention runbook and replace "Last exercised: never".
