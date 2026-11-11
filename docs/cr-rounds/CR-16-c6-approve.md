# CR-16: C6 Approve + DesignVersion

- Status: **open** (implementation complete; merge pending)
- Slices: `C6`
- Milestone progress: M2 → toward M3 (C6)

## What shipped

- Domain `DesignVersion` + `Design.Approve` / `CloneDraft`
- Migration `013_create_design_versions` (`metaldogs_design_versions`, UNIQUE `design_id`, FK `approved_version_id`)
- `ApproveBound` TX + `POST …/approve` (idempotent) and `POST …/clone` (source byte Put-copy, new owner token)
- Sync job `prepare_production_artwork` terminals `pending_spec` (`MaxRetries=1`); conflict: leave failed/done alone (no `Admin.Retry`); cancelled → pending `attempts=0`; stale processing → pending/`run_at` only (attempts preserved); zero-row re-read: failed/done/young processing OK, still-stale processing → error; skip enqueue when ready; enqueue fail → 503 + `version_id`
- Approve hashes artwork; `ApproveBound` locks design/artwork; extend (GREATEST) source/artwork/mockup `delete_after` to `expires_at`; append-only + artwork FK (`014`)
- Clone: owner_ref / size / checksum / sniff; SoftDelete-first cleanup (no blob delete on SoftDelete fail) + ClearStorageKey; concurrency cap

## Verification results

| Item | Result |
| --- | --- |
| Domain approve/clone/version | Pass locally |
| Application approve + idempotent + clone | Pass locally |
| HTTP approve/clone feature | Pass locally |
| Postgres ApproveBound unique + status race | Pass with DSN (`:5433`) |
| Prepare handler pending_spec | Pass locally |
| Docs links | Pass locally |

## Tests added

- `domain/design_test.go` — Approve, CloneDraft, NewDesignVersion, ProductionStatuses
- `application/design_test.go` — `TestDesignService_ApproveAndClone`
- `httpapi/designs_test.go` — `TestDesigns_ApproveAndClone`
- `adapters/postgres/design_version_repo_test.go` — ApproveBound race/unique
- `sync_prepare_production_test.go` — pending_spec; failed/done left alone; young processing; stale SQL leaves attempts; race to failed preserves attempts; cancelled resets attempts; reclaim reread treats failed/done as success
- Route/SyncJobs asserts in `sync_obs_test.go`

## Flags / migrations / rollback

- Flagged via existing `plugins.metaldogs.enabled`
- Migration `013_create_design_versions.sql` (expand: versions table + FK)
- Rollback: revert CR; drop versions FK/table after traffic stop if needed

## Documentation updated

- [x] ROADMAP C6 DONE; api.md approve/clone; ADR-0005 Accepted (DesignVersion); generation-pipeline + design-to-order prepare stub; CR-16; CR log; plugin README

## Follow-ups

- G5 + P0: real prepare_production_artwork handler
- E4: OrderDesignSnapshot (ADR-0005 remainder)
- S4 storefront approve UI; C7 regen cooldown UI
