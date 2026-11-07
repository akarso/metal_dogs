# CR-05: F4 private object storage

- Closed: 2026-11-07
- Slices: `F4`
- Milestone progress: M0 → 5/6 slices (`F0`–`F4`; `D0` remains)

## What shipped

- `ports.AssetStore` + `ObjectKey` (`metaldogs/{asset_type}/{yyyy}/{mm}/{asset_id}.{ext}`)
- `adapters/localfs` private root + HMAC `SignedGetURL` shim (`GET /api/customizer/dev-assets/{token}`)
- `adapters/mediastore` wrap of Shopanda `media.SignedURLStorage` when composition root registered S3 (or other signer)
- Plugin Init wires store; registers shim only for local fallback; **no Put call sites**
- ADR-0004 → Accepted (metadata table + EXIF strip deferred to C2+)

## Verification results

| Item | Result |
| --- | --- |
| Local Put/Head/SignedGetURL / bad+expired MAC / missing | `adapters/localfs` contract test |
| Mediastore wrap | `adapters/mediastore` fake SignedURLStorage |
| ObjectKey validation | `ports` tests |
| ImportBoundary | Pass |

## Flags / migrations / rollback

- Env (local shim): `SHOPANDA_PLUGINS_METALDOGS_ASSETS_PATH` (default `var/metaldogs-assets`), `SHOPANDA_PLUGINS_METALDOGS_ASSET_SIGN_SECRET` (random per process if unset).
- No DB migrations. Rollback: disable plugin / revert CR.

## Documentation updated

- [x] ROADMAP F4 DONE; ADR-0004 Accepted; plugin README; local-dev; overview deviations; this CR

## Follow-ups

- C2+: `metaldogs_assets`, EXIF strip, ownership 404 feature tests, production IAM/private-bucket runbook detail
- F3 pin absorb (#568) and F2 billing gate still open
- `D0` design session store
