# CR-12: C2 uploads API

- Status: **open** — PR [#24](https://github.com/akarso/metal_dogs/pull/24) (not closed until merge + green CI)
- Slices: `C2`
- Milestone progress: M2 → C2 (pending merge)

## What shipped

- Migration `003_create_assets.sql` (`metaldogs_assets`, `UNIQUE(storage_key)`, mime check, nullable `owner_ref`)
- `domain.Asset`, `ports.AssetRepository` (Insert/GetByID/SoftDelete), postgres `AssetRepo`, shared `generation/imagemime.Sniff`
- `application/imageprep`: sniff → `DecodeConfig` bounds/pixel gate → Orientation normalize → EXIF-strip re-encode
- `UploadService`: Put + insert + signed GET; compensating blob Delete (+ SoftDelete on sign failure); in-process concurrency cap
- Public `POST /api/customizer/uploads` streams multipart `file`; response splits `preview_url_expires_at` vs `delete_after`
- WebP rejected `415`; retention stub `delete_after=+2h` (sweep = O2)

## Verification results

| Item | Result |
| --- | --- |
| Unit sniff / DecodeConfig IHDR bomb / Orientation=6 / EXIF strip | Pass locally |
| Application cleanup + concurrency + classified prepare errors | Pass locally |
| httptest happy / MIME / WebP / dims / corrupt / 413 / missing / nil 503 | Pass locally |
| DSN Insert/Get/SoftDelete + unique storage_key | Pass with `SHOPANDA_TEST_DSN` when available |
| Docs links | Pass locally |
| GitHub Actions CI | May still be org/billing-blocked (empty steps); re-run when Actions execute |

## Tests added

- `generation/imagemime`, `application/imageprep` (incl. IHDR bomb + Orientation=6), `application/upload_test.go`
- `httpapi/uploads_test.go` (status-code matrix)
- `adapters/postgres/asset_repo_test.go`
- Plugin / registerPlugins / sync_obs assert uploads route

## Flags / migrations / rollback

- Migration `plugin:metaldogs:003_create_assets.sql` (expand-only; amended before first prod apply)
- No new flags; requires media + DB; concurrency cap is in-process only (O4 for IP limits)
- Rollback: revert CR; drop table if never written in prod

## Documentation updated

- [x] ROADMAP C2 pending merge (honest O2/O4 gaps); C1 DONE; api.md uploads + TTL split; ADR-0004 Put note; plugin README; CR-12; CR log

## Follow-ups

- C3: bind `owner_ref` + capability token; refuse attach of foreign/unowned uploads
- O2: `retention_sweep` for expired unattached uploads (and orphan blobs without rows)
- O4: IP / route rate limits beyond in-process semaphore
- C5+: real `thumbnail_url` via asset ingest
