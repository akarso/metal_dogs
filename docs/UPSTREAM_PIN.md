# Upstream pin

| Field | Value |
| --- | --- |
| Upstream remote | `upstream` → `https://github.com/akarso/shopanda.git` |
| Origin (this product) | `origin` → `git@github.com:akarso/metal_dogs.git` (HTTPS clone URL: `https://github.com/akarso/metal_dogs.git`) |
| Branch policy | **main-only** (no Metal Dogs `dev` branch; do not run upstream `scripts/promote-dev-to-main.sh` here) |
| Pinned ref | commit SHA (Shopanda has no release tags yet) |
| Pinned commit | `5681894658c44ce344b2df930effecda7ffd43c4` |
| Pinned subject | Merge pull request #565 from akarso/dev |
| Verified locally | 2026-11-05 (F0 baseline); docs/CI contract repaired 2026-11-06 |

Upgrade procedure: [`runbooks/shopanda-upgrade.md`](runbooks/shopanda-upgrade.md).

Metal Dogs owns product documentation under `docs/` (roadmap, ADRs, architecture, runbooks). Upstream Shopanda guides/diagrams/phase history are **not** vendored as full trees; stubs under `docs/guides/` and `docs/diagrams/` point at this pin on GitHub. Inherited fail-closed artefacts kept in-tree: `docs/GOVULN_BASELINE.md`, `docs/COMMERCIAL.md`.

Before merging a newer `upstream` tip: ensure this clone is not shallow (`test ! -f .git/shallow`). If it is, `git fetch --unshallow upstream` first.

**Forward-ports ahead of this pin** (absorb on the next upgrade slice; expect conflicts with in-tree platform edits until then):

- [shopanda#568](https://github.com/akarso/shopanda/pull/568) — sync-job observability (F3). Merged to `dev` as `e1903b5c`.
- [shopanda#570](https://github.com/akarso/shopanda/pull/570) — local SignedURLStorage, `BindMediaStorage`, `PrivateBlobsPrefix` (F4; product HMAC shim removed). Merged to `dev` as `50f812d6` (**stops at** `09642d99`; not sufficient alone).
- [shopanda#571](https://github.com/akarso/shopanda/pull/571) — **required** #570 CR closes: `./var/media` Load/`install.sh` default, HMAC token v1 + DEPLOYMENT handler-first rollout, private mode/symlink tests. Merged to `dev` as [`a8cbcb18`](https://github.com/akarso/shopanda/commit/a8cbcb18c5054178ba5e5f0e4980f82b0a7ed512) (contains `39684035`). Next pin/upgrade absorb must land at **≥ `a8cbcb18`** (or any tip that includes it)—do not absorb #570 without #571.
