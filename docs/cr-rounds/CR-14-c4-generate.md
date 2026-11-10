# CR-14: C4 generate job + status polling

- Status: **open** (implementation complete; close on merge + green CI)
- Slices: `C4`
- Milestone progress: M2 → C4

## What shipped

- Migration `008_create_generation_jobs.sql` (active-job + idempotency unique indexes)
- Domain `GenerationJob` machine + `Design.RequestGeneration` / preview/failed terminals
- `GenerateService` + `GenerateWorker` (fake provider, G3 validate, artwork asset, null `owner_ref`)
- `RegisterManual("generate")` → `integration.sync.metaldogs.generate` (post-commit Enqueue)
- Routes `POST /api/customizer/designs/{id}/generate`, `GET /api/customizer/generation-jobs/{id}`
- Config `plugins.metaldogs.generation.{provider,max_retries,max_attempts_per_design}` (fake-only)
- AssetStore `Read` via `media.ReadableStorage`
- ADR-0003 Accepted

## Verification results

| Item | Result |
| --- | --- |
| Unit job/design transitions + retry classification | Pass locally |
| Feature generate → worker fake → preview_ready; crash skip provider | Pass locally |
| Idempotency-Key replay + HTTP 202/400/404 | Pass locally |
| DSN mutex + idempotency unique | Pass with `SHOPANDA_TEST_DSN` when available |
| Docs links | Pass locally |

## Tests added

- `domain/generation_job_test.go`
- `application/generate_test.go`
- `httpapi/generate_test.go`
- `adapters/postgres/generation_job_repo_test.go`

## Flags / migrations / rollback

- Migration `plugin:metaldogs:008_create_generation_jobs.sql`
- Config generation.* (provider must be `fake` in C4 or Init fails)
- Rollback: revert CR; drop `metaldogs_generation_jobs` if unused

## Documentation updated

- [x] ROADMAP C4 DONE; C3 DONE; ADR-0003 Accepted; api.md generate/jobs; CR-14; CR log; runbook

## Follow-ups

- C5: mockup compose + signed artwork/mockup URLs
- C7: remaining-attempts UI + cooldown
- OpenAI live provider path (ADR-0008)
- `metaldogs:design-reconcile` / attach lost provider result
- Adversarial review closed: enqueue replay while queued, last-attempt terminalization, validating reclaim, artwork `owner_ref`=(design,type), `409` includes `job_id`, source preflight; live-claim Conflict no-op; in-tx `OnRequeued` claim fence at BindQueue/`jobs:retry` (missing hook aborts); claim re-check before Generate/writes (lost token → SkipOutcome; cancel/re-read fail → requeue product out of running); attempts-fenced `Complete`/`Fail` + `ErrSkipOutcome`; ctx-honoring Put/Read
