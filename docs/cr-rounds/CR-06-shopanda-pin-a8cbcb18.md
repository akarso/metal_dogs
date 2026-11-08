# CR-06: Shopanda pin absorb (`a8cbcb18`)

- Closed: 2026-11-08 (docs/bookkeeping); **origin CI not green** — Actions jobs empty-fail / no runner (billing). Do not treat as merge-ready until required checks execute or a recorded billing waiver exists.
- Slices: pin upgrade (absorb #568/#570/#571 merges; retain F3 platform residuals)
- Milestone progress: M0 → F0–F4 on pin `a8cbcb18…`; `D0` still checklist-only

## What shipped

- Merged explicit upstream SHA [`a8cbcb18`](https://github.com/akarso/shopanda/commit/a8cbcb18c5054178ba5e5f0e4980f82b0a7ed512) (`upstream/dev` tip; **not** on `upstream/main` yet).
- Absorbs [shopanda#568](https://github.com/akarso/shopanda/pull/568), [#570](https://github.com/akarso/shopanda/pull/570), [#571](https://github.com/akarso/shopanda/pull/571), plus [#566](https://github.com/akarso/shopanda/pull/566) product-packaging (`sitekit`, `scripts/product-init`, `scripts/ci`).
- Kept Metal Dogs docs stubs (no vendored upstream guides, diagrams, or phase trees).
- CR follow-up harden: re-listed F3 residuals (`BoundQueue`, sync reinjection, cache-control, product registration tests); fail-closed `release.yml` image name; reserve `metaldogs` slug; `make docs-policy` → MD hardened checker; previous-pin full-SHA + bare `guides/` link scans; absorb canary script.

## Residual deviations (not “absorb complete”)

These still differ from pin `a8cbcb18` and are required for F3 health/correlation until upstreamed or the MD facade is rewritten — see [`../architecture/overview.md`](../architecture/overview.md):

- `App.BoundQueue()` (`internal/platform/plugin/providers.go`)
- Sync-handler payload `request_id` → ctx reinjection (`internal/application/integration/sync_runtime.go`)
- `cmd/api/cache_control_prefixes.go`, MetalDogs register/migrate/config tests

## Verification results

| Item | Result |
| --- | --- |
| `go build -o app ./cmd/api` | Pass (local) |
| ImportBoundary / `plugins/metaldogs/...` / media / sitekit | Pass (local) |
| Product docs links + previous-pin SHA scan | Pass (local) |
| Absorb canary (`.github/scripts/check-upstream-deviations.sh`) | Pass (local) |
| Origin Actions (`unit`, `integration`, `integration-metaldogs`, `govuln`, `docs-policy`, `docs-main`) | **Red / empty** — no runner; merge-blocking |

## Flags / migrations / rollback

- No Metal Dogs flag/migration changes.
- Rollback: revert this upgrade merge; previous pin `56818946…`.

## Documentation updated

- [x] `UPSTREAM_PIN.md`, ROADMAP, overview deviations (residuals restored), CR-04/CR-05 follow-ups, guides/diagrams stubs
- [x] This CR (honest absorb + CI status)

## Follow-ups

- Unblock or waive origin Actions before merge; do not close on local-only evidence.
- Upstream `BoundQueue` + sync reinjection (or delete residuals and adapt MD facade).
- When Shopanda promotes `dev` → `main` past `a8cbcb18`, prefer a containing `main` tip.
- F2 billing gate / require `CI / integration-metaldogs` still open
