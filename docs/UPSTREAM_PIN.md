# Upstream pin

| Field | Value |
| --- | --- |
| Upstream remote | `upstream` → `https://github.com/akarso/shopanda.git` |
| Origin (this product) | `origin` → `git@github.com:akarso/metal_dogs.git` (HTTPS clone URL: `https://github.com/akarso/metal_dogs.git`) |
| Branch policy | **main-only** (no Metal Dogs `dev` branch; do not run upstream `scripts/promote-dev-to-main.sh` here) |
| Pinned ref | commit SHA (Shopanda has no release tags yet) |
| Pinned commit | `565dde5a284ea3983b431e915acb4a37ae166a3c` |
| Pinned subject | Merge pull request #617 from akarso/dev |
| Verified locally | 2026-11-18 (explicit SHA from `upstream/main`) |

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
- [shopanda#590](https://github.com/akarso/shopanda/pull/590) — `pending_line`, `DiscardEmptyGuestCart`, `ProductVisible` / `ClassifyProductSlug`, PR-1556–1558 (`1f500783`)
- [shopanda#592](https://github.com/akarso/shopanda/pull/592) — `App.BoundQueue()` and sync `request_id` reinjection, PR-1559 (`e2b418b8`)
- [shopanda#593](https://github.com/akarso/shopanda/pull/593) — `registerProductPlugins`, PR-1560 (`4e90b492`)
- [shopanda#594](https://github.com/akarso/shopanda/pull/594) — `pluginsdk.NoCache`, PR-1561 (`4cdb0caa`)
- [shopanda#595](https://github.com/akarso/shopanda/pull/595) — product-owned `merge=ours` / `Makefile.product` docs, PR-1562 (`5874ee36`)
- [shopanda#597](https://github.com/akarso/shopanda/pull/597) — `plugins.<slug>` Extra config, PR-1563 (`39f104ca`)
- [shopanda#598](https://github.com/akarso/shopanda/pull/598) — `CatalogSeedPort`, PR-1564 (`246b5bde`)
- [shopanda#599](https://github.com/akarso/shopanda/pull/599) — attempt-aware `Complete`/`Fail`, `ErrSkipOutcome`, PR-1565a (`28b6695b`)
- [shopanda#602](https://github.com/akarso/shopanda/pull/602) — `OnQueueBound`, `OnRequeued`, `Queue.Requeue`, PR-1565b (`f94f0894`)
- [shopanda#605](https://github.com/akarso/shopanda/pull/605) — catalog seed Status/Type/Attributes, PR-1566 (`b026d581`)
- [shopanda#606](https://github.com/akarso/shopanda/pull/606) — promote that `dev` tip onto `main` (`7c982b00`)
- [shopanda#608](https://github.com/akarso/shopanda/pull/608) — `CatalogPriceSeed.Update`, PR-1567 (`850e3a3f`)
- [shopanda#610](https://github.com/akarso/shopanda/pull/610) — NoCache path-param prefixes and `Config.Extra` nested flatten, PR-1568/1569 (`c17f407a`, follow-ups through `9d3ef379`)
- [shopanda#611](https://github.com/akarso/shopanda/pull/611) — promote that `dev` tip onto `main` (`ab35e965`)
- [shopanda#612](https://github.com/akarso/shopanda/pull/612) — MetalDogs config via Extra and skip empty Extra env, PR-1570/1571 (`852e15a1`)
- [shopanda#614](https://github.com/akarso/shopanda/pull/614) — `CapCatalogSeed` CLI-only and attribute mismatch values, PR-1572/1573 (`1b39989f`, harden `73348d5e`)
- [shopanda#615](https://github.com/akarso/shopanda/pull/615) — promote that `dev` tip onto `main` (`7bd34bef`)
- [shopanda#616](https://github.com/akarso/shopanda/pull/616) — plugin CLI auto-binds `CapCatalogSeed` for the owning plugin, PR-1574 (`8032b225`, harden through `dd277dd4`)
- [shopanda#617](https://github.com/akarso/shopanda/pull/617) — promote that `dev` tip onto `main` (`565dde5a`)

**Still Metal Dogs–only** (do not claim the deviations are gone): our copies of `BoundQueue`, sync `request_id` reinjection, the C4 attempt fence, and `Admin.RequeueLease` were kept on this pin. Upstream now ships the same extension points; later UPG4 tickets delete the copies. Typed MetalDogs config and empty `SHOPANDA_PLUGINS_*` overlays are fixed upstream (PR-1570/1571). `CapCatalogSeed` is CLI-only (PR-1572) and auto-bound on `RequiresDB` commands whose plugin declared the Need (PR-1574). `cmd/api/cli_plugins.go` matches this pin. Listed under [`architecture/overview.md`](architecture/overview.md) and checked by `.github/scripts/check-upstream-deviations.sh`. `pkg/pluginsdk` matches this pin. `pluginsdk.Host` `Cart()` and `Catalog()` are what Metal Dogs add-to-cart uses ([CR-20](cr-rounds/CR-20-mig1-cart-pluginsdk.md)). `cmd/api/metaldogs_cart.go` and `cmd/api/metaldogs_catalog.go` are removed. `metaldogs:catalog-seed` calls `Host.CatalogSeed().Import` once, with `CatalogPriceSeed.Update`. Attribute mismatch errors include the stored and requested values (PR-1573).
