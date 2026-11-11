# CR-17: C7 Regeneration limits

- Status: **open** (implementation complete; merge pending)
- Slices: `C7`
- Milestone progress: M3 → C7

## What shipped

- Design DTO exposes `generation_attempts`, `generation_attempts_remaining`, `max_generation_attempts`
- Config `plugins.metaldogs.generation.cooldown_seconds` (default 30; env `SHOPANDA_PLUGINS_METALDOGS_GENERATION_COOLDOWN_SECONDS`)
- Domain helpers for remaining attempts + cooldown; generate checks cooldown after transition eligibility and before bumping attempts
- `429` `cooldown` with computed `Retry-After`; existing `429` `limit_reached` when attempts exhausted (no job created)
- ROADMAP marks C5/G4 DONE (CR-15 already merged)

## Verification results

| Item | Result |
| --- | --- |
| Domain remaining + cooldown | Pass locally |
| Generate 4th attempt / cooldown | Pass locally |
| HTTP limit mapping | Pass locally |
| Docs links | Pass locally |

## Tests added

- `domain/generation_job_test.go` — remaining + cooldown helpers
- `application/generate_test.go` — fourth attempt; cooldown then success
- `httpapi/generate_test.go` — `429` `limit_reached`
- `httpapi/designs_test.go` — attempt budget on create

## Flags / migrations / rollback

- Config-only; no migration
- Rollback: revert CR / set cooldown_seconds=0 still uses domain default when wired ≤0 at plugin Init (set a large value only via config if operators need a longer gap)

## Documentation updated

- [x] ROADMAP C7 DONE; api.md attempt budget + cooldown; CR-17; CR log; plugin README

## Follow-ups

- Provider-fault attempts that should not consume customer budget (runbook note)
- S4 UI remaining-count / cooldown copy
