# Upstream pin

| Field | Value |
| --- | --- |
| Upstream remote | `upstream` → `https://github.com/akarso/shopanda.git` |
| Origin (this product) | `origin` → `git@github.com:akarso/metal_dogs.git` (HTTPS clone URL: `https://github.com/akarso/metal_dogs.git`) |
| Branch policy | **main-only** (no Metal Dogs `dev` branch; do not run upstream `scripts/promote-dev-to-main.sh` here) |
| Pinned ref | commit SHA (Shopanda has no release tags yet) |
| Pinned commit | `a411c970bbc869d54d44852bc30f7427be9b2745` |
| Pinned subject | Merge pull request #588 from akarso/pr-1554-1555-product-consumer-followups |
| Verified locally | 2026-11-16 (explicit SHA). Fetched `upstream/main` tip `9a8d2b7c` (PR #589) has the same tree |

Upgrade procedure: [`runbooks/shopanda-upgrade.md`](runbooks/shopanda-upgrade.md).

Metal Dogs owns product documentation under `docs/` (roadmap, ADRs, architecture, runbooks). Upstream Shopanda guides/diagrams/phase history are **not** vendored as full trees; stubs under `docs/guides/` and `docs/diagrams/` point at this pin on GitHub. Inherited fail-closed artefacts kept in-tree: `docs/GOVULN_BASELINE.md`, `docs/COMMERCIAL.md`.

Before merging a newer `upstream` tip: ensure this clone is not shallow (`test ! -f .git/shallow`). If it is, `git fetch --unshallow upstream` first.

**Merged on this pin** (Shopanda PRs now reachable from the pin SHA):

- [shopanda#568](https://github.com/akarso/shopanda/pull/568) — sync-job observability (`e1903b5c`)
- [shopanda#570](https://github.com/akarso/shopanda/pull/570) — local SignedURLStorage / `BindMediaStorage` / `PrivateBlobsPrefix` (`50f812d6`)
- [shopanda#571](https://github.com/akarso/shopanda/pull/571) — media harden follow-up (`a8cbcb18`, includes `39684035`)
- [shopanda#566](https://github.com/akarso/shopanda/pull/566) — product-packaging (`sitekit`, `scripts/product-init`, `scripts/ci`)
- [shopanda#573](https://github.com/akarso/shopanda/pull/573) — public catalog price and on-hand stock (`479e4c44`)
- [shopanda#574](https://github.com/akarso/shopanda/pull/574) — promote that `dev` tip onto `main` (`43f0a1ed`)
- [shopanda#575](https://github.com/akarso/shopanda/pull/575) — `Access.PluginOnlyWrite` enforcement and field owners (`dd40dc7f`)
- [shopanda#576](https://github.com/akarso/shopanda/pull/576) — bind extension-field owner after plugin Init (`b13147b8`)
- [shopanda#577](https://github.com/akarso/shopanda/pull/577) — `ValueAPI` vs core value ops (`7ec75c6a`)
- [shopanda#578](https://github.com/akarso/shopanda/pull/578) — omit plugin-only fields from storefront and admin forms (`810400eb`)
- [shopanda#579](https://github.com/akarso/shopanda/pull/579) — retire plugin-only extension fields (`d173264d`)
- [shopanda#580](https://github.com/akarso/shopanda/pull/580) — strip plugin-write trust at pricing, checkout, and job dispatch (`4b164072`)
- [shopanda#581](https://github.com/akarso/shopanda/pull/581) — `make check` gofmt/vet (`d19ee742`)
- [shopanda#582](https://github.com/akarso/shopanda/pull/582) — promote that `dev` tip onto `main` (`f58a4157`)
- [shopanda#584](https://github.com/akarso/shopanda/pull/584) — public extension SDK, PR-1540–1552 (`4f9de269`)
- [shopanda#586](https://github.com/akarso/shopanda/pull/586) — `CartPort` consumer follow-ups, PR-1553 (`cec3e492`)
- [shopanda#588](https://github.com/akarso/shopanda/pull/588) — optional customer identity and `CatalogPort` slug, PR-1554/1555 (`a411c970`)

**Still Metal Dogs–only** (do not claim full F3 absorb): `App.BoundQueue()`, sync-handler `request_id`→ctx reinjection, plus registration/cache-control wiring — listed under [`architecture/overview.md`](architecture/overview.md) and checked by `.github/scripts/check-upstream-deviations.sh`. Two `pkg/pluginsdk` test files differ from this pin for the same reason: `sdk_test.go` (`Complete`/`Fail` attempt argument) and `harness/harness_test.go` (upstream `TestAdaptInit_CartPortBound` does not compile against `CartPort` and expects `AddItem` where `Host.Cart()` fail-closes). `pluginsdk.Host` `Cart()` and `Catalog()` are what Metal Dogs add-to-cart uses ([CR-20](cr-rounds/CR-20-mig1-cart-pluginsdk.md)). `cmd/api/metaldogs_cart.go` is removed.
