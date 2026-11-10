# CR-13: C3 designs + ownership tokens

- Status: **open** — PR pending (not closed until merge + green CI)
- Slices: `C3`
- Milestone progress: M2 → C3 (pending merge)

## What shipped

- Migration `004_create_designs.sql` (`metaldogs_designs`, status CHECK, `owner_token_hash bytea`)
- Domain Design status machine + `ValidateDesignTexts` (ascii/latin/latin_polish)
- `DesignService` Create/Get/Patch; token mint (256-bit → base64url once; SHA-256 stored)
- Asset `BindOwnerRef` / `ClearOwnerRef`; CreateBound txn
- Routes `POST/GET/PATCH /api/customizer/designs` with Bearer auth (404 on miss)
- ADR-0006 / ADR-0007 Accepted

## Verification results

| Item | Result |
| --- | --- |
| Unit transitions / texts / token | Pass locally |
| httptest create/auth/patch/422/503 | Pass locally |
| DSN CreateBound + bind exclusivity | Pass with `SHOPANDA_TEST_DSN` when available |
| Docs links | Pass locally |

## Tests added

- `domain/design_test.go`, `application/design_test.go`, `application/token_test.go`
- `httpapi/designs_test.go`
- `adapters/postgres/design_repo_test.go`
- Plugin / registerPlugins / sync_obs assert design routes

## Flags / migrations / rollback

- Migration `plugin:metaldogs:004_create_designs.sql`
- No new flags; media + DB required
- Rollback: revert CR; drop table if unused in prod

## Documentation updated

- [x] ROADMAP C3 pending merge; C2 DONE; api.md designs; ADR-0006/0007 Accepted; CR-13; CR log; README

## Follow-ups

- C4: generate job + status polling
- C5: artwork/mockup signed URLs
