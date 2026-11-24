# ADR-0004: Private S3-compatible storage with short-lived signed URLs

- **Status:** Accepted (F4 port; **C2** first Put call site via `POST /api/customizer/uploads`). **E5** landed ([CR-35](../cr-rounds/CR-35-e5-artwork-access.md), 2026-11-23): `OrderArtworkAccess` signs approved artwork with `SignedGetURL` and production artwork with attachment via `SignedGetURLDisposition`. TTL is a fixed 300s (still capped at 15m). The route and `metaldogs:order-artwork` register only when `plugins.metaldogs.order_snapshot.enabled` is true. Each issued URL is one append-only row in `metaldogs_artwork_access_log` (no URL, no storage key). URLs are not logged. A non-UUID order id is logged at info; error is reserved for `ErrArtworkAccessFailed`. **E6a** ([CR-37](../cr-rounds/CR-37-e6a-order-thumbnail.md), 2026-11-24) is the one exception to the 15-minute signed-URL rule: see amendment below.
- **Date:** 2026-10-15
- **Related slices:** `F4`, `C2`, `C5`, `E5`, `E6a`, `E6b`, `O2`, `O6`
- **Related ADRs:** [ADR-0007](0007-guest-design-ownership-tokens.md)

## Context

Uploaded photos are private personal data. Shopanda provides `media.SignedURLStorage` for S3 and local (`GET /api/v1/media/local-signed/{token}`), `PrivateBlobsPrefix` / `IsPrivatePath`, and `BindMediaStorage` ([shopanda#570](https://github.com/akarso/shopanda/pull/570)).

## Decision

1. Metal Dogs assets use core media storage (local or S3). No second product registry or HMAC route.
2. Object keys: `private/blobs/metaldogs/{asset_type}/{yyyy}/{mm}/{asset_id}.{ext}` (`ports.AssetKeyPrefix`). The mediastore adapter **rejects** any other key for Put/Head/Delete/SignedGetURL.
3. Delivery via short-lived signed URLs (`SignedGetURL` caps TTL ≤ 15m, default 60s). Inline disposition only for allowlisted image extensions (`jpg/jpeg/png/webp`); other keys force attachment. E5 production artwork is the exception: `OrderArtworkAccess` calls `SignedGetURLDisposition` with attachment, so a production image is a download. Caller `Content-Type` outside that allowlist is overridden. URLs must not be logged.
4. Plugin `AssetStore` wraps `media.SignedURLStorage` only.
5. `metaldogs_assets` metadata + EXIF-strip re-encode on upload: **landed in C2**. Guest ownership / 404 hiding of foreign assets: **C3+**. Retention sweep of expired unattached uploads: **O2**.
6. Local layout: config default and examples use `./var/media` (not under a static web root). Private keys share that root — operators must **never** FileServer `base_path` / `base_url`. Platform localfs uses `0700`/`0600` (chmod ancestors on private write), EvalSymlinks containment, and versioned HMAC tokens with v0 compat parse.

## Amendment (E6a): stable order-thumbnail URL

`GET /api/customizer/order-thumbnails/{token}` does not use `SignedGetURL`. The handler reads the thumbnail through `AssetStore` and writes the bytes. The token is `base64url(order_id|variant_id).base64url(HMAC-SHA256(key, "order-thumbnail:" + order_id + "|" + variant_id))`. It has no expiry. HMAC compare is constant time. The route is registered only when `order_snapshot.enabled` is true and `thumbnail_signing_key` is at least 32 characters.

This is the one exception to the 15-minute rule. It is safe because the image is a PNG whose longest edge is at most 320 px, the token is unguessable without the key, and neither the URL nor the response contains a storage key or a signed storage URL. A bad token, an unknown line, a snapshot with no thumbnail, and a missing object share one `404` body. The handler does not log the token. The platform HTTP access log (and any proxy log) records the full request path, so the token appears there on every hit: treat those logs as credential-bearing for this route. Platform gap, not fixable in the plugin: the access log should use the route pattern or redact the path per route.

## Honest limits (not yet fail-closed)

- S3 “private” is ACL/Cache-Control under `IsPrivatePath`; a public bucket policy can still expose keys — production IAM/bucket policy is operator-owned (runbook / O2+).
- Log redaction of `local-signed/` / `X-Amz-Signature` is policy, not automated yet.
- First product Put call site: `application.UploadService` (C2 / CR-12).

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Public bucket with unguessable keys | Still public |
| Product `/api/customizer/dev-assets` HMAC | Superseded by platform #570 |
| Serve all bytes through the API | Bandwidth; signed URLs preferred |

## Consequences

- Positive: namespace isolation at the adapter; platform local/S3 signing; preview-friendly inline disposition.
- Negative: local privacy still depends on not statically serving the media root; S3 public policies remain an ops footgun until detectable fail-closed checks land.
