# ADR-0004: Private S3-compatible storage with short-lived signed URLs

- **Status:** Accepted for port wiring (F4 / CR-05); isolation/enforcement incomplete until C2 writers + ops checks below
- **Date:** 2026-10-15
- **Related slices:** `F4`, `C2`, `C5`, `E5`, `E6`, `O2`, `O6`
- **Related ADRs:** [ADR-0007](0007-guest-design-ownership-tokens.md)

## Context

Uploaded photos are private personal data. Shopanda provides `media.SignedURLStorage` for S3 and local (`GET /api/v1/media/local-signed/{token}`), `PrivateBlobsPrefix` / `IsPrivatePath`, and `BindMediaStorage` ([shopanda#570](https://github.com/akarso/shopanda/pull/570)).

## Decision

1. Metal Dogs assets use core media storage (local or S3). No second product registry or HMAC route.
2. Object keys: `private/blobs/metaldogs/{asset_type}/{yyyy}/{mm}/{asset_id}.{ext}` (`ports.AssetKeyPrefix`). The mediastore adapter **rejects** any other key for Put/Head/Delete/SignedGetURL.
3. Delivery via short-lived signed URLs (`SignedGetURL` caps TTL ≤ 15m, default 60s; inline Content-Disposition for customizer preview). URLs must not be logged.
4. Plugin `AssetStore` wraps `media.SignedURLStorage` only.
5. `metaldogs_assets` metadata + EXIF strip + ownership 404s: **C2+**.
6. Local layout: prefer `media.local.base_path` outside any static web root (example: `./var/media`). Private keys share that root — operators must **never** FileServer `base_path` / `base_url`. Platform localfs uses `0700`/`0600` for private paths and EvalSymlinks containment.

## Honest limits (not yet fail-closed)

- S3 “private” is ACL/Cache-Control under `IsPrivatePath`; a public bucket policy can still expose keys — production IAM/bucket policy is operator-owned (runbook / C2+).
- Log redaction of `local-signed/` / `X-Amz-Signature` is policy, not automated yet.
- No Put call sites until C2; port + prefix guard + contract tests ship in F4.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Public bucket with unguessable keys | Still public |
| Product `/api/customizer/dev-assets` HMAC | Superseded by platform #570 |
| Serve all bytes through the API | Bandwidth; signed URLs preferred |

## Consequences

- Positive: namespace isolation at the adapter; platform local/S3 signing; preview-friendly inline disposition.
- Negative: local privacy still depends on not statically serving the media root; S3 public policies remain an ops footgun until detectable fail-closed checks land.
