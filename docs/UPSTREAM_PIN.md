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

- [shopanda#568](https://github.com/akarso/shopanda/pull/568) — sync-job observability (F3)
- [shopanda#570](https://github.com/akarso/shopanda/pull/570) — local SignedURLStorage, `BindMediaStorage`, `PrivateBlobsPrefix` (F4; product HMAC shim removed)
