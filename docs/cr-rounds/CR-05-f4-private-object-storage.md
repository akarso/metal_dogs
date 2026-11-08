# CR-05: F4 private object storage

- Closed: 2026-11-07
- Revised: 2026-11-07 (absorb #570; prefix guard + contract test + ADR honesty)
- Slices: `F4`
- Milestone progress: M0 → 5/6 slices (`F0`–`F4`; `D0` remains)

## What shipped

- `ports.AssetStore` + `ObjectKey` / `AssetKeyPrefix` (`private/blobs/metaldogs/...`)
- `adapters/mediastore` wrap with **hard key prefix reject** + TTL cap + inline disposition
- Platform: local SignedURL disposition flag; private path `0700`/`0600`; EvalSymlinks containment; `SaveWithContentType` on S3/local
- Contract test: `ObjectKey` → Put → SignedGetURL → Handler GET
- Init tests against a fake `SignedURLStorage` (no `*localfs` dependency)
- **No Put call sites**; metadata/EXIF/ownership deferred to C2+

## Verification results

| Item | Result |
| --- | --- |
| Prefix reject / TTL cap / inline | `adapters/mediastore` |
| Localfs round-trip | `TestStore_LocalfsContractRoundTrip` |
| ObjectKey + PrivateBlobsPrefix alignment | `ports` tests |
| Init without media / fake media | `plugin_test` |
| ImportBoundary | Pass |

## Flags / migrations / rollback

- `SHOPANDA_MEDIA_LOCAL_BASE_PATH` prefer `./var/media` (never FileServer)
- `SHOPANDA_MEDIA_LOCAL_SIGN_SECRET` (≥32 bytes) outside DEV
- No DB migrations

## Documentation updated

- [x] ROADMAP / ADR-0004 (honest limits) / CR-05 / local-dev / README / UPSTREAM_PIN (#570 + #571)

## Follow-ups

- C2+: writers, EXIF, ownership 404, log redaction lint, S3 public-policy fail-closed if detectable
- Pin absorb: next upgrade must include [shopanda#571](https://github.com/akarso/shopanda/pull/571) merge [`a8cbcb18`](https://github.com/akarso/shopanda/commit/a8cbcb18c5054178ba5e5f0e4980f82b0a7ed512) (not #570/`50f812d6` alone). #571 is already on Shopanda `dev`; product tree still forward-ports until that pin bump.
