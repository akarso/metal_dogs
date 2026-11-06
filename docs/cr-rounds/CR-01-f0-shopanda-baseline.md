# CR-01: F0 Shopanda baseline

- Closed: 2026-11-06 (re-closed after F0-fix docs/CI contract)
- Slices: `F0` (+ F0-fix: docs/CI/link/job-type/bring-up honesty)
- Milestone progress: M0 → 1/6 slices (`F0` done; `F1`–`F4`, `D0` remain)

## What shipped

Metal Dogs repository is a Shopanda fork pinned to `5681894658c44ce344b2df930effecda7ffd43c4` via git remote `upstream` → `https://github.com/akarso/shopanda.git`. Product remote remains `origin` → `metal_dogs` (**main-only** branch policy). Empty `plugins/metaldogs/` and `storefront/` placeholders. Product docs under `docs/`; upstream guides/diagrams not fully vendored (stubs + GitHub links at pin). Fail-closed `docs/GOVULN_BASELINE.md` and `docs/COMMERCIAL.md` restored for CI/licensing. ADR-0001 Accepted. Local Shopanda WIP checkout was not used as a merge source.

## Verification results

Source inspection at the pin (not a full runtime contract suite unless noted):

| Item | Status |
| --- | --- |
| Plugin `Migrator` + namespaced migrations + advisory lock | Inspected at pin |
| `RegisterManual` / `Enqueue` (`integration.sync.{slug}.{name}`) | Inspected at pin |
| Cart-line extension field snapshot | Inspected at pin |
| `order.created` / `order.failed` published | Inspected at pin |
| `ApplyShipmentStatus` for integrations | Inspected at pin |
| Guest checkout, Stripe, S3, media `SignedURLStorage`, SSR slots | Inspected at pin |
| Import boundary | Contract-tested (`go test ./plugins/ -run ImportBoundary`) |
| `go build -o app ./cmd/api` | Exercised |
| Docs-policy script on Metal Dogs layout | Exercised (`bash .github/scripts/check-product-docs-links.sh`) |
| Govuln baseline file present | Exercised (fail-closed script requires `docs/GOVULN_BASELINE.md`) |
| `./app setup` / `./app dev` against compose Postgres | **Not** exercised in F0 (bring-up sequence documented; follow-up when Postgres available) |

## Tests added

- Upstream ImportBoundary on the pin.
- No Metal Dogs plugin tests yet (plugin empty).
- Docs-policy path list updated; local run required before push.

## Flags / migrations / rollback

- No plugin registration; no behaviour change vs stock Shopanda at the pin.
- Rollback: revert the merge / F0-fix commits; keep `GOVULN_BASELINE.md` if CI must stay green.

## Documentation updated

- [x] ADR-0001 → Accepted; `UPSTREAM_PIN.md`; ROADMAP §1 status honesty (Inspected at pin)
- [x] Job types reconciled to `integration.sync.metaldogs.*` (ADR-0003, architecture, runbooks, ROADMAP)
- [x] `runbooks/shopanda-upgrade.md` (unshallow prerequisite), `runbooks/local-development.md` (copy-paste JWT + DEV_MODE + Postgres 17)
- [x] `AGENTS.md` remotes + main-only; root `README.md` / `RUNBOOK.md` / `PLUGINS.md` link honesty
- [x] `architecture/overview.md` Upstream deviations expanded
- [x] CR-01 linked from `cr-rounds/README.md` log
- [x] `.cursor/rules/pr-documentation-checklist.mdc` Metal Dogs main-only
- [ ] Full `./app setup` + `./app dev` smoke — deferred until local Postgres compose exercise (document remaining)

## Follow-ups filed

- `F1`: register plugin behind flag; `Migrator` + health route; accept ADR-0003 with runtime verification notes.
- Planned upgrade slice: bump pin to current `upstream/main` (Phase 17 packaging) after F0 is green on CI; run the pin-URL rewrite sweep in `shopanda-upgrade.md`.
- Rename GHCR / release image from `shopanda` to a Metal Dogs product name **before the first version tag**.
- Prove `./app setup` + `./app dev` against compose Postgres 17; treat full CI (`unit` / `integration` / `govuln`) as the real merge gate on first push.
- Track E public SDK: wait until Metal Dogs has a working external plugin (upstream intent).
