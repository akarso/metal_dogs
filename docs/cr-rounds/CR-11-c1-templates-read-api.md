# CR-11: C1 templates read API

- Closed: 2026-11-09
- Slices: `C1`
- Milestone progress: M2 → C1 done (C2+ / G4 still open)

## What shipped

- `ports.TemplateStore.ListPublished` / `GetPublishedByID` + postgres adapter (+ DSN tests)
- Public routes `GET /api/customizer/templates` and `GET /api/customizer/templates/{id}` (UUID); drafts excluded; unknown genre → empty list; bad/missing id → `404`
- Public DTO omits prompts / model params / hashes / reference paths; `thumbnail_url` always `null`
- Handlers resolve DB from `app.Bootstrap` per request (`503` if missing); unit tests inject catalog
- `docs/architecture/api.md` created; G3/CR-10 marked fully merged

## Verification results

| Item | Result |
| --- | --- |
| httptest DTO shape / draft exclusion / genre / 404 | Pass |
| Plugin route registration | Pass |
| DSN ListPublished / GetPublishedByID | Pass with `SHOPANDA_TEST_DSN` |
| Docs links | Pass |

## Tests added

- `httpapi/templates_test.go` — secrets leak guard, genre filter, 404 cases
- `adapters/postgres/template_store_test.go` — list/get published round-trip
- Plugin / registerPlugins assert new route patterns

## Flags / migrations / rollback

- No new migrations or flags
- Rollback: revert CR; remove routes

## Documentation updated

- [x] ROADMAP C1 DONE; G3 DONE; api.md; plugin README; CR-11; CR log; CR-10 closed note

## Follow-ups

- C2+: real `thumbnail_url` via asset ingest + signed GET
- C3 designs; S1 gallery consumes list DTO
