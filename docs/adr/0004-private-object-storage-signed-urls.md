# ADR-0004: Private S3-compatible storage with short-lived signed URLs

- **Status:** Accepted (F4 port; **C2** first Put call site via `POST /api/customizer/uploads`)
- **Date:** 2026-10-15
- **Related slices:** `F4`, `C2`, `C5`, `E5`, `E6`, `O2`, `O6`
- **Related ADRs:** [ADR-0007](0007-guest-design-ownership-tokens.md)

## Context

Uploaded photos are private personal data. Shopanda provides `media.SignedURLStorage` for S3 and local (`GET /api/v1/media/local-signed/{token}`), `PrivateBlobsPrefix` / `IsPrivatePath`, and `BindMediaStorage` ([shopanda#570](https://github.com/akarso/shopanda/pull/570)).

## Decision

1. Metal Dogs assets use core media storage (local or S3). No second product registry or HMAC route.
2. Object keys: `private/blobs/metaldogs/{asset_type}/{yyyy}/{mm}/{asset_id}.{ext}` (`ports.AssetKeyPrefix`). The mediastore adapter **rejects** any other key for Put/Head/Delete/SignedGetURL.
3. Delivery via short-lived signed URLs (`SignedGetURL` caps TTL ≤ 15m, default 60s). Inline disposition only for allowlisted image extensions (`jpg/jpeg/png/webp`); other keys force attachment. Caller `Content-Type` outside that allowlist is overridden. URLs must not be logged.
4. Plugin `AssetStore` wraps `media.SignedURLStorage` only.
5. `metaldogs_assets` metadata + EXIF-strip re-encode on upload: **landed in C2**. Guest ownership / 404 hiding of foreign assets: **C3+**. Retention sweep of expired unattached uploads: **O2**.
6. Local layout: config default and examples use `./var/media` (not under a static web root). Private keys share that root — operators must **never** FileServer `base_path` / `base_url`. Platform localfs uses `0700`/`0600` (chmod ancestors on private write), EvalSymlinks containment, and versioned HMAC tokens with v0 compat parse.

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
