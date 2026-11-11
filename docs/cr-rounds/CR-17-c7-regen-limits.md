# CR-17: C7 Regeneration limits

- Status: **open** (implementation complete; merge pending)
- Slices: `C7`
- Milestone progress: M3 → C7

## What shipped

- Design DTO: `generation_attempts`, `generation_attempts_remaining`, `max_generation_attempts`, `retry_after` (cooldown lockout only; `0` ≠ “may regenerate”)
- Config `plugins.metaldogs.generation.cooldown_seconds` (default 30; `0` disables; env `SHOPANDA_PLUGINS_METALDOGS_GENERATION_COOLDOWN_SECONDS`; reject &lt;0 or &gt;24h at Init)
- Hard cap checked **before** cooldown; spent budget → `429` `limit_reached` with **no** `Retry-After`
- Cooldown → `429` `cooldown` with header + JSON `retry_after` (ceil seconds, clamped to cooldown); anchor is terminal `finished_at` only
- Missing latest job with attempts &gt; 0 → `503` `unavailable` on generate **and** design GET (fail closed)
- `BeginGenerating` fences on prior `generation_attempts` + `latest_job_id`; PersistArtwork allows artwork replace under the same claim
- **Policy:** every `RequestGeneration` (incl. after `generation_failed`) consumes budget and cooldown; provider-fault refunds remain a follow-up
- ROADMAP: G4/C5 marked **partial** (web-sized mockup base still open)

## Verification results

| Item | Result |
| --- | --- |
| Domain remaining + cooldown + CooldownAnchor | Pass locally |
| Limit inside cooldown window | Pass locally |
| Cooldown then success / same-key replay | Pass locally |
| HTTP limit (no Retry-After) + cooldown mapping | Pass locally |
| Design GET retry_after during cooldown / spent budget | Pass locally |
| Init cooldown 0 / -1 / 86401 | Pass locally |
| DSN PersistArtwork replace + stale latest fence | Pass with DSN (`:5433`) |
| Docs links | Pass locally |

## Tests added

- `domain/generation_job_test.go` — remaining, cooldown, ceil/clamp, CooldownAnchor (finished_at only)
- `application/generate_test.go` — fourth attempt; limit inside window; cooldown; missing latest; terminal null finished_at; in-flight job_id
- `application/design_test.go` — approve view cooldown / cooldown-disabled preview I/O → 503 + version_id; PATCH post-commit view includes committed design + cause; GetByID error preserves cause
- `httpapi/generate_test.go` — `429` limit without Retry-After; `429` cooldown/`job_id` for finished_at wait and in-flight
- `httpapi/designs_test.go` — attempt budget; GET retry_after; GET+PATCH missing latest → 503; approve view-fail 503 has `version_id` and no `design`
- `adapters/postgres/generation_job_repo_test.go` — stale `latest_job_id` fence; PersistArtwork replace under claim
- `plugin_test.go` — Init cooldown bounds

## Flags / migrations / rollback

- Config-only; no migration
- Rollback: revert CR / set `cooldown_seconds: 0` to disable wait

## Documentation updated

- [x] ROADMAP C7 DONE; G4/C5 partial; api.md; generation-pipeline; CR-17; CR log; plugin README; runbooks

## Follow-ups

- Provider-fault attempts that should not consume customer budget
- S4 UI remaining-count / cooldown copy
