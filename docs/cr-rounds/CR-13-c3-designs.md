# CR-13: C3 designs + ownership tokens

- Status: **DONE** — merged PR [#28](https://github.com/akarso/metal_dogs/pull/28) (2026-11-10)
- Closed: 2026-11-10
- Slices: `C3`
- Milestone progress: M2 → C3 done

## What shipped

- Migration `004_create_designs.sql` (`metaldogs_designs`, status CHECK, `owner_token_hash bytea`)
- Migration `005_design_bind_constraints.sql` (UNIQUE active `owner_ref`; `generating` on expires index)
- Migration `006_soft_deleted_asset_blob_idx.sql` (soft-deleted blob sweep index)
- Migration `007_asset_storage_key_nullable.sql` (clear `storage_key` after blob removed)
- Domain Design status machine + `ValidateDesignTexts` (ascii / latin / latin_polish)
- `DesignService` Create/Get/Patch; token mint (256-bit → base64url once; SHA-256 stored)
- CreateBound / ReplaceSource (`FOR UPDATE`, soft-delete prior source) / TouchActivity
- Routes `POST/GET/PATCH /api/customizer/designs` with Bearer auth (404 on miss/expiry)
- `AuthMiddleware` skips JWT parse on `/api/customizer/*` Bearer
- Preview sign soft-fail after commit (token always returned); UUID transport checks
- ADR-0006 / ADR-0007 Accepted

## Verification results

| Item | Result |
| --- | --- |
| Unit transitions / texts / token / sign soft-fail / pinned retired PATCH | Pass locally |
| httptest AuthMiddleware stack + create/auth/patch/409/422/400/503 | Pass locally |
| DSN CreateBound + bind exclusivity + concurrent ReplaceSource | Pass with `SHOPANDA_TEST_DSN` when available |
| Docs links | Pass locally |

## Tests added

- `domain/design_test.go`, `application/design_test.go`, `application/token_test.go`
- `httpapi/designs_test.go` (incl. AuthMiddleware mount)
- `adapters/postgres/design_repo_test.go` (incl. concurrent replace)
- `storefront/auth_middleware_test.go` customizer Bearer skip
- Plugin / registerPlugins / sync_obs assert design routes

## Flags / migrations / rollback

- Migrations `plugin:metaldogs:004_create_designs.sql`, `005_design_bind_constraints.sql`
- No new flags; media + DB required
- Rollback: revert CR; drop index/table if unused in prod

## Documentation updated

- [x] ROADMAP C3 pending merge; C2 DONE; api.md designs; ADR-0006/0007 Accepted; CR-13; CR log; README

## Follow-ups

- Idempotency-Key for lost `201` (token still hash-only; needs response replay store)
- Access-log capture assertion that Bearer never lands in query/path logs
- O2 sweep must purge soft-deleted rows still holding `storage_key` (index in mig 006)
- C4: generate job + status polling
- C5: artwork/mockup signed URLs
