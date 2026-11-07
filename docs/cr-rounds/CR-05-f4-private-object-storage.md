# CR-05: F4 private object storage

- Closed: 2026-11-07
- Revised: 2026-11-07 (absorb [shopanda#570](https://github.com/akarso/shopanda/pull/570) — drop product HMAC shim)
- Slices: `F4`
- Milestone progress: M0 → 5/6 slices (`F0`–`F4`; `D0` remains)

## What shipped

- `ports.AssetStore` + `ObjectKey` (`private/blobs/metaldogs/{asset_type}/{yyyy}/{mm}/{asset_id}.{ext}`)
- `adapters/mediastore` wrap of Shopanda `media.SignedURLStorage` (BoundMediaStorage or registered MediaStorage)
- Platform local signed GET (`GET /api/v1/media/local-signed/{token}`) / S3 presign — **no** `/api/customizer/dev-assets` shim
- Plugin Init wires store when core media is registered; **no Put call sites**
- ADR-0004 → Accepted (metadata table + EXIF strip deferred to C2+)

## Verification results

| Item | Result |
| --- | --- |
| Mediastore wrap | `adapters/mediastore` fake SignedURLStorage |
| ObjectKey under PrivateBlobsPrefix | `ports` tests |
| Init without media fails; no dev-assets route | `plugin_test` |
| Platform localfs / redirect allowlist | cherry-picked #570 packages |
| ImportBoundary | Pass |

## Flags / migrations / rollback

- Platform: `SHOPANDA_MEDIA_LOCAL_SIGN_SECRET` (≥32 bytes) when `media.storage=local` outside `SHOPANDA_DEV_MODE`.
- No DB migrations. Rollback: disable plugin / revert CR.

## Documentation updated

- [x] ROADMAP F4 DONE; ADR-0004 Accepted; plugin README; local-dev; overview deviations; this CR; UPSTREAM_PIN #570 note

## Follow-ups

- C2+: `metaldogs_assets`, EXIF strip, ownership 404 feature tests, production IAM/private-bucket runbook detail
- Pin absorb: merge #570 (and #568) into the consumed UPSTREAM_PIN SHA on the next upgrade slice
- `D0` design session store
