# CR-11: C1 templates read API

- Status: **open** — PR [#23](https://github.com/akarso/metal_dogs/pull/23) (not closed until merge + green CI)
- Slices: `C1`
- Milestone progress: M2 → C1 (pending merge)

## What shipped

- `ports.PublishedTemplate` public-read model; postgres `ListPublished` / `GetPublishedByID` select only DTO columns (no prompt/model/hash/path)
- Public routes `GET /api/customizer/templates` and `GET /api/customizer/templates/{id}`; http-owned nested DTOs (no embedded domain types)
- `TemplateCatalog` wired once in plugin Init when `Bootstrap.DB` is set; nil catalog → `503 unavailable`
- Drafts and retired excluded; unknown genre → empty list; bad/missing id → `404`
- `thumbnail_url` always `null` until asset ingest
- `docs/architecture/api.md` (genre exact/case-sensitive)

## Verification results

| Item | Result |
| --- | --- |
| httptest DTO shape / draft+retired / genre / 404 / 503 / 500 | Pass locally |
| Plugin route registration | Pass locally |
| DSN ListPublished / GetPublishedByID (+ retired) | Pass with `SHOPANDA_TEST_DSN` locally |
| Docs links | Pass locally |
| GitHub Actions CI | **Blocked** — jobs complete in ~3s with empty steps (org/billing; also red on `main` after #22). Re-run required when Actions execute again. |

## Tests added

- `httpapi/templates_test.go` — list/get secret guards, retired 404, nil catalog 503, store error 500, genre 200
- `adapters/postgres/template_store_test.go` — list/get published + retired exclusion
- Plugin / registerPlugins / sync_obs assert template route patterns

## Flags / migrations / rollback

- No new migrations or flags
- Rollback: revert CR; remove routes

## Documentation updated

- [x] ROADMAP C1 pending merge; api.md; plugin README; CR-11; CR log

## Follow-ups

- Unblock GitHub Actions billing so DSN CI jobs actually run
- C2+: real `thumbnail_url` via asset ingest + signed GET
- C3 designs; S1 gallery consumes list DTO (exact genre casing)
