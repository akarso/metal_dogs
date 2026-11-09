# CR-12: C2 uploads API

- Status: **open** — PR pending (not closed until merge + green CI)
- Slices: `C2`
- Milestone progress: M2 → C2 (pending merge)

## What shipped

- Migration `003_create_assets.sql` (`metaldogs_assets` + `delete_after` index)
- `domain.Asset`, `ports.AssetRepository`, postgres `AssetRepo`, shared `generation/imagemime.Sniff`
- `application/imageprep` (JPEG/PNG decode bounds + EXIF-strip re-encode) and `UploadService`
- Public `POST /api/customizer/uploads` (multipart `file`) → Put + insert + 60s signed preview URL; `delete_after=+2h` stub (sweep = O2)
- WebP rejected `415`; unauthenticated until C3 design attach

## Verification results

| Item | Result |
| --- | --- |
| Unit sniff / bounds / EXIF strip | Pass locally |
| httptest happy / wrong MIME / missing / nil 503 | Pass locally |
| DSN Insert/GetByID (+ soft-delete hide) | Pass with `SHOPANDA_TEST_DSN` when available |
| Docs links | Pass locally |
| GitHub Actions CI | May still be org/billing-blocked (empty steps); re-run when Actions execute |

## Tests added

- `generation/imagemime`, `application/imageprep`, `application/upload_test.go`
- `httpapi/uploads_test.go`
- `adapters/postgres/asset_repo_test.go`
- Plugin / registerPlugins / sync_obs assert uploads route

## Flags / migrations / rollback

- Migration `plugin:metaldogs:003_create_assets.sql` (expand-only)
- No new flags; requires media + DB like templates
- Rollback: revert CR; drop table if never written in prod

## Documentation updated

- [x] ROADMAP C2 pending merge; C1 finalized DONE; api.md uploads; ADR-0004 Put note; plugin README; CR-12; CR log

## Follow-ups

- C3: attach `upload_id` + capability token
- O2: `retention_sweep` for expired unattached uploads
- Real `thumbnail_url` on templates via asset ingest
