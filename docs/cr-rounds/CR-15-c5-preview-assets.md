# CR-15: G4 mockup compose + C5 preview assets

- Status: **open** (in progress; G4/C5 remain open until web-sized mockup base)
- Slices: `G4`, `C5`
- Milestone progress: M2 → toward C5

## What shipped (partial)

- `generation/mockup.Compose` with embedded **80×100 placeholder** black-tee + default print area (not customer web-sized yet)
- Generate worker: `PersistArtwork` before mockup; compose/key/decode → permanent; mockup Put → transient; prior soft-deleted blobs cleaned after success
- Design DTO signed `artwork_preview_url` / `mockup_preview_url` when job belongs to design and assets match owner_ref + type; lookup I/O → 500
- `Succeed` requires mockup id; `ForceFail` clears artwork + mockup ids

## Verification results

| Item | Result |
| --- | --- |
| Unit compose placement/bounds/letterbox+alpha | Pass locally |
| Worker fake → artwork + mockup; resume without re-generate | Pass locally |
| Design GET ownership/type/signer/I/O cases | Pass locally |
| Postgres PersistArtwork + CompleteSuccess/FinalizeSuccess mockup replace | Pass with DSN |
| Docs links | Pass locally |

## Tests added

- `generation/mockup/compose_test.go`
- `application/design_test.go` (`Get_ArtworkAndMockupPreviews`)
- `adapters/postgres/generation_job_repo_test.go` (`PersistArtworkAndCompleteSuccessMockup`)
- Extended `application/generate_test.go`

## Flags / migrations / rollback

- No new migration
- Flagged via existing `plugins.metaldogs.enabled`
- Rollback: revert CR

## Documentation updated

- [x] ROADMAP G4 + C5 remain **open** (honest placeholder base); api.md; generation-pipeline; CR-15; CR log; plugin README

## Follow-ups

- Ship a real web-sized tee base (or stop calling the preview web-sized) and close G4/C5
- Drive print area from template `output_config`
- C6: approve + DesignVersion freeze
- G5: production artwork (distinct from mockup preview)
