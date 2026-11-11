# CR-17: C7 Regeneration limits

- Status: **open** (implementation complete; merge pending)
- Slices: `C7`
- Milestone progress: M3 → C7

## What shipped

- Design DTO: `generation_attempts`, `generation_attempts_remaining`, `max_generation_attempts`, `retry_after`
- Config `plugins.metaldogs.generation.cooldown_seconds` (default 30; `0` disables; env `SHOPANDA_PLUGINS_METALDOGS_GENERATION_COOLDOWN_SECONDS`; reject &lt;0 or &gt;24h at Init)
- Hard cap checked **before** cooldown; spent budget → `429` `limit_reached` with **no** `Retry-After`
- Cooldown → `429` `cooldown` with header + JSON `retry_after` (ceil seconds, clamped to cooldown)
- Missing latest job with attempts &gt; 0 → `503` `unavailable` (fail closed)
- `BeginGenerating` fences on prior `generation_attempts` + `latest_job_id`
- **Policy:** every `RequestGeneration` (incl. after `generation_failed`) consumes budget and cooldown; provider-fault refunds remain a follow-up
- ROADMAP: G4/C5 marked **partial** (web-sized mockup base still open)

## Verification results

| Item | Result |
| --- | --- |
| Domain remaining + cooldown + LastAttemptAt | Pass locally |
| Limit inside cooldown window | Pass locally |
| Cooldown then success / same-key replay | Pass locally |
| HTTP limit (no Retry-After) + cooldown mapping | Pass locally |
| Docs links | Pass locally |

## Tests added

- `domain/generation_job_test.go` — remaining, cooldown, ceil/clamp, LastAttemptAt
- `application/generate_test.go` — fourth attempt; limit inside window; cooldown; missing latest
- `httpapi/generate_test.go` — `429` limit without Retry-After; `429` cooldown with `retry_after=25`
- `httpapi/designs_test.go` — attempt budget on create

## Flags / migrations / rollback

- Config-only; no migration
- Rollback: revert CR / set `cooldown_seconds: 0` to disable wait

## Documentation updated

- [x] ROADMAP C7 DONE; G4/C5 partial; api.md; generation-pipeline; CR-17; CR log; plugin README; runbooks

## Follow-ups

- Provider-fault attempts that should not consume customer budget
- S4 UI remaining-count / cooldown copy
