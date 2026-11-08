# Upstream pin

| Field | Value |
| --- | --- |
| Upstream remote | `upstream` → `https://github.com/akarso/shopanda.git` |
| Origin (this product) | `origin` → `git@github.com:akarso/metal_dogs.git` (HTTPS clone URL: `https://github.com/akarso/metal_dogs.git`) |
| Branch policy | **main-only** (no Metal Dogs `dev` branch; do not run upstream `scripts/promote-dev-to-main.sh` here) |
| Pinned ref | commit SHA (Shopanda has no release tags yet) |
| Pinned commit | `a8cbcb18c5054178ba5e5f0e4980f82b0a7ed512` |
| Pinned subject | Merge pull request #571 from akarso/pr-570-media-harden-followup |
| Verified locally | 2026-11-08 (absorb #568 + #570 + #571; explicit `upstream/dev` tip — not yet on `upstream/main`) |

Upgrade procedure: [`runbooks/shopanda-upgrade.md`](runbooks/shopanda-upgrade.md).

Metal Dogs owns product documentation under `docs/` (roadmap, ADRs, architecture, runbooks). Upstream Shopanda guides/diagrams/phase history are **not** vendored as full trees; stubs under `docs/guides/` and `docs/diagrams/` point at this pin on GitHub. Inherited fail-closed artefacts kept in-tree: `docs/GOVULN_BASELINE.md`, `docs/COMMERCIAL.md`.

Before merging a newer `upstream` tip: ensure this clone is not shallow (`test ! -f .git/shallow`). If it is, `git fetch --unshallow upstream` first.

**Merged on this pin** (Shopanda PRs now reachable from the pin SHA):

- [shopanda#568](https://github.com/akarso/shopanda/pull/568) — sync-job observability (`e1903b5c`)
- [shopanda#570](https://github.com/akarso/shopanda/pull/570) — local SignedURLStorage / `BindMediaStorage` / `PrivateBlobsPrefix` (`50f812d6`)
- [shopanda#571](https://github.com/akarso/shopanda/pull/571) — media harden follow-up (`a8cbcb18`, includes `39684035`)
- [shopanda#566](https://github.com/akarso/shopanda/pull/566) — product-packaging (`sitekit`, `scripts/product-init`, `scripts/ci`)

**Still Metal Dogs–only** (do not claim full F3 absorb): `App.BoundQueue()`, sync-handler `request_id`→ctx reinjection, plus registration/cache-control wiring — listed under [`architecture/overview.md`](architecture/overview.md) and checked by `.github/scripts/check-upstream-deviations.sh`.
