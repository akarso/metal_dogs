# CR-12: C2 uploads API

- Status: **closed** — merged via PR [#24](https://github.com/akarso/metal_dogs/pull/24) (`1623e378`)
- Closed: 2026-11-10
- Slices: `C2`
- Milestone progress: M2 → C2 done

## What shipped

- Migration `003_create_assets.sql` (`metaldogs_assets`, `UNIQUE(storage_key)`, mime check, nullable `owner_ref`)
- `domain.Asset`, `ports.AssetRepository`, postgres `AssetRepo`, shared `generation/imagemime.Sniff`
- `application/imageprep`: DecodeConfig gate, Orientation normalize, EXIF-strip re-encode
- `UploadService` Put + insert + signed GET; SoftDelete-first cleanup; `OnCleanupError` logging; concurrency cap
- Public `POST /api/customizer/uploads`

## Follow-ups

- C3: bind `owner_ref` + capability token (this round closes the schema hook)
- O2: retention sweep + orphan blob reconcile
- O4: IP / route rate limits beyond in-process semaphore
