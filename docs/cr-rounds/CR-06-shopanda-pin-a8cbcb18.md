# CR-06: Shopanda pin absorb (`a8cbcb18`)

- Closed: 2026-11-08
- Slices: pin upgrade (absorb F3/F4 platform forward-ports)
- Milestone progress: M0 → F0–F4 closed on absorbed pin; `D0` still checklist-only

## What shipped

- Merged explicit upstream SHA [`a8cbcb18`](https://github.com/akarso/shopanda/commit/a8cbcb18c5054178ba5e5f0e4980f82b0a7ed512) (`upstream/dev` tip; not yet on `upstream/main`).
- Absorbs [shopanda#568](https://github.com/akarso/shopanda/pull/568), [#570](https://github.com/akarso/shopanda/pull/570), [#571](https://github.com/akarso/shopanda/pull/571), plus [#566](https://github.com/akarso/shopanda/pull/566) product-packaging (`sitekit`, `scripts/product-init`, `scripts/ci`).
- Dropped forward-port deviation row for sync-job observability; retargeted guide/diagram GitHub links to the new pin.
- Kept Metal Dogs docs stubs (no vendored upstream guides, diagrams, or phase trees).

## Verification results

| Item | Result |
| --- | --- |
| `go build -o app ./cmd/api` | Pass |
| ImportBoundary | Pass |
| `go test ./plugins/metaldogs/...` | Pass |
| Product docs links | Pass |

## Flags / migrations / rollback

- No Metal Dogs flag/migration changes.
- Rollback: revert this upgrade merge; previous pin `56818946…`.

## Documentation updated

- [x] `UPSTREAM_PIN.md`, ROADMAP §1/F3/F4, overview deviations, CR-04/CR-05 follow-ups, guides/diagrams stubs, `shopanda-upgrade.md` last-exercised
- [x] This CR + log link

## Follow-ups

- When Shopanda promotes `dev` → `main` past `a8cbcb18`, prefer pinning a `main` tip that still contains this SHA (or newer).
- F2 billing gate / require `CI / integration-metaldogs` still open
