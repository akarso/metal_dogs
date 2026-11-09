# CR-09: G2 template definition format v1

- Closed: 2026-11-09
- Slices: `G2`
- Milestone progress: M1 → G0/G1/G2 scaffold complete (G1 paid exit gate still open)

## What shipped

- Migration `002_create_templates.sql` → `metaldogs_templates` (+ partial unique index: one `published` per slug)
- Domain `Template` Validate (required model, unique editable keys) + normalized content hash; unknown YAML/JSON fields rejected
- Application catalog: LoadDraft no-op only on matching **draft** hash (retries on version unique conflicts); Publish/Retire exclusive transactions; editable keys normalized before validate/hash
- Postgres `TemplateStore` + DSN round-trip tests (UUID ids)
- CLI `metaldogs:templates validate|load|publish|retire`
- Five spike templates as YAML under `plugins/metaldogs/templates/`
- Eval loads Template v1; sample dog name via `-dog-name` (default Burek)

## Verification results

| Item | Result |
| --- | --- |
| Domain validation / hash / required model | Pass |
| Application catalog (in-memory fake store) | Pass |
| Postgres load / version bump / publish / retire / reload-after-retire | Pass with `SHOPANDA_TEST_DSN` (UUID ids; partial unique published index) |
| Eval v1 + fake | Pass |
| Docs links | Pass |
| Publish of authored set | **Not done** (safe delivery: drafts only) |

## Tests added

- `domain/template_test.go` — table-driven Validate; hash stability; nil vs empty fixed
- `application/template_catalog_test.go` — noop/bump, publish retires drafts, load-after-retire, explicit `-version` rollback, unknown YAML fields, path jail
- `adapters/postgres/template_store_test.go` — DSN round-trip with `ports.UUIDGenerator`
- `generation/eval` — LoadTemplate v1 YAML; Run with v1 + `-dog-name`

## Flags / migrations / rollback

- Migration: `plugin:metaldogs:002_create_templates.sql` (expand-only table)
- No HTTP surface; no published templates
- Rollback: revert CR; drop table via reverse migrate if needed

## Documentation updated

- [x] ROADMAP G2 DONE; domain-model Template section; template-publishing runbook; generation-quality paths; plugin README; CR-09; CR log

## Follow-ups

- Operator: G1 paid spike using v1 YAML paths; accept ADR-0008 if pass
- C1: list/detail DTOs over `published` only
- Asset ingest to populate `reference_asset_id` / `thumbnail_asset_id`
