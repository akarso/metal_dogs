# CR-01: F0 Shopanda baseline

- Closed: 2026-11-05
- Slices: `F0`
- Milestone progress: M0 → 1/6 slices (`F0` done; `F1`–`F4`, `D0` remain)

## What shipped

Metal Dogs repository is a Shopanda fork pinned to `5681894658c44ce344b2df930effecda7ffd43c4` via git remote `upstream` → `https://github.com/akarso/shopanda.git`. Product remote remains `origin` → `metal_dogs`. Empty `plugins/metaldogs/` and `storefront/` placeholders. Metal Dogs owns `docs/`; upstream Shopanda docs are not vendored. ADR-0001 Accepted. Local Shopanda WIP checkout was not used as a merge source.

## Verification results

Re-verified against the pin (Shopanda Phase 13 + PR-1400 on `upstream/main`):

- Plugin `Migrator` + namespaced migrations + advisory lock — Verified
- `RegisterManual` / `Enqueue` (`integration.sync.{slug}.{name}`) — Verified
- Cart-line extension field snapshot — Verified
- `order.created` / `order.failed` published — Verified
- `ApplyShipmentStatus` for integrations — Verified
- Guest checkout, Stripe, S3, media `SignedURLStorage`, SSR slots — Verified

## Tests added

- Upstream ImportBoundary exercised on the pin (`go test ./plugins/ -run ImportBoundary -count=1`).
- No Metal Dogs plugin tests yet (plugin empty).

## Flags / migrations / rollback

- No plugin registration; no behaviour change vs stock Shopanda at the pin.
- Rollback: revert the merge commit / redeploy previous Metal Dogs revision (docs-only history before this round).

## Documentation updated

- [x] ADR-0001 → Accepted; `UPSTREAM_PIN.md`; ROADMAP §1 capability table
- [x] `runbooks/shopanda-upgrade.md`, `runbooks/local-development.md`
- [x] `AGENTS.md` remotes + build commands; root `README.md` Metal Dogs banner
- [x] `architecture/overview.md` Upstream deviations table
- [ ] `D0` full diagram pass — deferred; overview deviations only this round

## Follow-ups filed

- `F1`: register plugin behind flag; `Migrator` + health route; record ADR-0003 verification (job type naming `integration.sync.metaldogs.*`).
- Keep Shopanda improvements in the Shopanda repo; pull into Metal Dogs only via `git fetch upstream` on planned upgrade slices.
