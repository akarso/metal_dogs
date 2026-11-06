# Runbook: upgrading the pinned Shopanda version

- **Introduced by slice:** `F0`
- **Last exercised:** never (baseline set 2026-11-05)
- **Severity when triggered:** medium (planned change with regression risk)
- **Owner:** backend maintainer

## Symptom / trigger

A new upstream commit (or future tag) with security fixes, a needed feature, or a change to an extension point we use. Upgrades are planned slices, never done ad hoc.

## Remotes and pin

| Remote | URL | Role |
| --- | --- | --- |
| `origin` | `https://github.com/akarso/metal_dogs.git` | This product repository (push here) |
| `upstream` | `https://github.com/akarso/shopanda.git` | Shopanda source; fetch-only for upgrades |

Current pin: [`docs/UPSTREAM_PIN.md`](../UPSTREAM_PIN.md).

Do **not** merge from a local Shopanda working tree. Always `git fetch upstream` from GitHub so WIP checkouts elsewhere cannot contaminate Metal Dogs.

**Shallow clone prerequisite:** if `.git/shallow` exists, run `git fetch --unshallow upstream` (or re-clone with full history) **before** merging. A shallow pin hides parent history and makes conflict diagnosis and `git merge-base` unreliable.

## Impact

Risk of contract drift in extension points (cart hook, checkout step, extension fields, events, job queue / sync jobs, storage port, shipment updater) and in migrations ordering. Metal Dogs owns `docs/`; upstream `docs/` changes must not overwrite product docs.

## Procedure

1. Read upstream commit range / release notes for `UPSTREAM_PIN..upstream/main` (or chosen SHA). List any change touching `plugin.App`, `plugin.Migrator`, `pluginsdk` sync jobs, events, extension fields, media storage, checkout workflow, shipment updater.
2. Create a branch `upgrade/shopanda-<shortsha>`.
3. Fetch and merge the pin candidate:

   ```bash
   git fetch upstream
   git merge <sha>   # prefer an explicit SHA until upstream tags exist
   ```

   Resolve conflicts:
   - **`docs/`** — keep Metal Dogs docs; discard upstream `docs/` additions (link to GitHub at the new pin instead).
   - **`AGENTS.md`** — keep Metal Dogs project rules; refresh the build/test command section from upstream if commands changed.
   - **`cmd/api/register_plugins.go`** — re-apply the `metaldogs` registration line if upstream touched the file.
   - All other conflicts: prefer upstream unless listed under "Upstream deviations" in `docs/architecture/overview.md`.
4. Update [`docs/UPSTREAM_PIN.md`](../UPSTREAM_PIN.md) to the new SHA.
5. Build and test:

   ```bash
   go build -o app ./cmd/api
   go test ./plugins/ -run ImportBoundary -count=1
   # then the full suite / contract tests for extension points we use
   ```
6. Run plugin migrations against a copy of the production schema; confirm expand/contract compatibility if workers may run mixed versions (`./app migrate`).
7. Deploy to staging; run the E2E flow with the fake provider; run the game-day subset for snapshots and fulfilment.
8. Update `docs/ROADMAP.md §1` if any verified capability changed; update ADR statuses if a decision is affected.
9. Deploy with the usual rollback plan (previous image; migrations are compatible by rule).

## Verification

- `./app dev` serves the default Shopanda storefront (and `/api/customizer/health` `200` once `F1` enables the plugin).
- Import-boundary test green; queue depth normal; an end-to-end order on staging produces a snapshot (from `E4` onward).

## Prevention / follow-up

- Keep deviations from upstream minimal and listed in `docs/architecture/overview.md`.
- Contract tests fail loudly on extension-point drift; add one whenever a new extension point is used.
- Never use `/path/to/local/shopanda` as `upstream` or as a merge source.
