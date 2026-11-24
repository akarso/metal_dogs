# Upstream pin

| Field | Value |
| --- | --- |
| Upstream remote | `upstream` → `https://github.com/akarso/shopanda.git` |
| Origin (this product) | `origin` → `git@github.com:akarso/metal_dogs.git` (HTTPS clone URL: `https://github.com/akarso/metal_dogs.git`) |
| Branch policy | **main-only** (no Metal Dogs `dev` branch; do not run upstream `scripts/promote-dev-to-main.sh` here) |
| Pinned ref | commit SHA (Shopanda has no release tags yet) |
| Pinned commit | `084d120660c50acabb045c1b19a78f25677d6843` |
| Pinned subject | Merge pull request #660 from akarso/pr-1603-order-line-extensions-email-ssr |
| Verified locally | 2026-11-24 (explicit SHA fetched from `upstream`; on `upstream/main` and the tip of `upstream/dev`. Tree equals both tips: `upstream/dev` `084d1206` and `upstream/main` `7d386c6b`, shopanda#661) |

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
- [shopanda#618](https://github.com/akarso/shopanda/pull/618) — shared process boot helpers `bootServePlugins` / `bootWorkerPlugins`, PR-1575 (`0bed9b1f`, harden `2e029ac4`)
- [shopanda#620](https://github.com/akarso/shopanda/pull/620) — `TestNewJobsRuntime_WarehousedemoReachesBind` fixture, PR-1578 (`6d253b50`)
- [shopanda#621](https://github.com/akarso/shopanda/pull/621) — `Host.PlatformConfig`, `pluginsdk.RequireQueueDriver`, PR-1577 (`58b87ea7`)
- [shopanda#622](https://github.com/akarso/shopanda/pull/622) — `jobs.WithRequeueTerminal()` for failed/done/cancelled, PR-1576 (`a2fc61f1`, harden through `2fbee1fa`)
- [shopanda#623](https://github.com/akarso/shopanda/pull/623) — promote that `dev` tip onto `main` (`de29a0c1`)
- [shopanda#624](https://github.com/akarso/shopanda/pull/624) — non-UUID job ids are not-found, PR-1579 (`50fd03b4`, harden through `a7da3d84`)
- [shopanda#625](https://github.com/akarso/shopanda/pull/625) — promote that `dev` tip onto `main` (`58aa6987`)
- [shopanda#626](https://github.com/akarso/shopanda/pull/626) — context-aware `ReadableStorage.Read`, PR-1580 (`32c937c6`, harden `52964e1d`)
- [shopanda#627](https://github.com/akarso/shopanda/pull/627) — promote that `dev` tip onto `main` (`abc82f51`)
- [shopanda#628](https://github.com/akarso/shopanda/pull/628) — cart extension fake is race-free; `scripts/ci/unit.sh` runs `./internal/application/cart/` with `-race`, PR-1581 (`b021817b`, harden `93aef832`)
- [shopanda#629](https://github.com/akarso/shopanda/pull/629) — promote that `dev` tip onto `main` (`fd4dd577`)
- [shopanda#630](https://github.com/akarso/shopanda/pull/630) — `pluginsdk.RequestIDFromContext`, PR-1582 (`aa1b4778`)
- [shopanda#631](https://github.com/akarso/shopanda/pull/631) — `extapi.ParseMinorUnits` and `IsValidCurrency`, PR-1583 (`6f2252c1`)
- [shopanda#632](https://github.com/akarso/shopanda/pull/632) — `CapJobs` / `Host.Jobs`, PR-1584 (`1aba998f`)
- [shopanda#633](https://github.com/akarso/shopanda/pull/633) — `CapMedia` / `Host.Media`, PR-1585 (`b5a297ac`)
- [shopanda#634](https://github.com/akarso/shopanda/pull/634) — `harness.OpenIsolated`, PR-1586 (`124239c3`)
- [shopanda#635](https://github.com/akarso/shopanda/pull/635) — `plugin.App` symbol map and exported `InjectRequestID`, PR-1587 (`987e621c`)
- [shopanda#636](https://github.com/akarso/shopanda/pull/636) — CapJobs/CapMedia bind-before-assert and ASCII `ParseMinorUnits`, PR-1588 (`8e98c0a8`)
- [shopanda#638](https://github.com/akarso/shopanda/pull/638) — `pluginsdk.MigrationSpec` aliases `plugin.MigrationSpec`, PR-1589 (`511b1542`)
- [shopanda#639](https://github.com/akarso/shopanda/pull/639) — `CapCatalogSeed` plugin may run `RequiresDB: false` siblings; `CLICommand.Needs`, PR-1590 (`3143e8f3`)
- [shopanda#640](https://github.com/akarso/shopanda/pull/640) — CapCatalogSeed CLI CR fixes, promote onto `main` (`ad14bd3a`)
- [shopanda#642](https://github.com/akarso/shopanda/pull/642) — `SyncJobs.Enqueue` `WithJobID`, promote onto `main` (`647c6a2a`)
- [shopanda#644](https://github.com/akarso/shopanda/pull/644) — `pluginsdk.JobsPort.CountsByType`, PR-1593 (`4bff865b`)
- [shopanda#645](https://github.com/akarso/shopanda/pull/645) — plugin CLI skips `CapJobs` and `CapMedia`, PR-1594 (`eb081c72`)
- [shopanda#646](https://github.com/akarso/shopanda/pull/646) — `WithJobID` terminal requeue race, PR-1595 (`d9ccc21c`)
- [shopanda#648](https://github.com/akarso/shopanda/pull/648) — product-owned path convention (owned / hook / companion), PR-1596 (`72785f27`). At that merge the tree equalled then-`upstream/main` `f0bf700f` (shopanda#649)
- [shopanda#650](https://github.com/akarso/shopanda/pull/650) — checkout API errors carry `plugin.<slug>`, a `details` object, and `503 unavailable` (`pluginsdk.WithClientCode`, `WithDetails`, `UnavailableError`, `IsUnavailable`; `httpx.ErrorBody.Details`), PR-1597 (`478c643a`)
- [shopanda#651](https://github.com/akarso/shopanda/pull/651) — SSR checkout confirms the Stripe PaymentIntent (Stripe.js on `checkout_confirm.html`, POST→303→GET confirm, `payment.stripe.publishable_key`, `GET /checkout/payment/return`), PR-1598 (`40e0d571`). At that merge the tree equalled then-`upstream/main` `5bdbe918` (shopanda#652)
- [shopanda#653](https://github.com/akarso/shopanda/pull/653) — `GET /checkout/payment/return` is on the product visibility out-of-scope list (`PublicProductVisibilityRoutes`), PR-1599 (`c25f3e7e`). At that merge the tree equalled then-`upstream/main` `d0fe14f1` (shopanda#654)
- [shopanda#656](https://github.com/akarso/shopanda/pull/656) — `OrderPort` `Lines`, `LinesForOrders` (extension values; line key `(order_id, variant_id)`), and `ListCreatedSince` (limit 1..100; core migration `112_orders_created_at_id_asc_index.sql`), PR-1600 (`9429cadf`)
- [shopanda#657](https://github.com/akarso/shopanda/pull/657) — `order.created` is published after the cart→order extension snapshot (`finishCreatedOrder`); a snapshot error still publishes, then returns the error; `Workflow.Execute` retries `create_order` once before compensate, PR-1601 (`5298259a`)
- [shopanda#658](https://github.com/akarso/shopanda/pull/658) — `pluginsdk.WithEventJobID` derives the `RegisterOnEvent` job id from the event payload, PR-1602 (`e062d4d3`). At that merge the tree equalled then-`upstream/main` `740f0b20` (shopanda#659)
- [shopanda#660](https://github.com/akarso/shopanda/pull/660) — `order_confirmed` lines and storefront order pages carry `VariantID`, `SKU`, and public `order_item` extensions (`WithOrderExtensions(ValueAPI)`), PR-1603 (`084d1206`). The SHA is on `upstream/main` and `upstream/dev`. Tree equals `upstream/dev` tip `084d1206` and `upstream/main` tip `7d386c6b` (shopanda#661)

**Still Metal Dogs–only** (do not claim the deviations are gone): product registration (`cmd/api/register_products.go` is owned and is not a deviation row; the registration test is), the Metal Dogs plugin, storefront docs, `.github/scripts` other than `run-integration-tests.sh`, `.cursor/rules/*`, and `.gitattributes`. Hook files (`Makefile`, `docker-compose.yml`, `.env.example`, `configs/config.example.yaml`, `scripts/ci/docs-policy.sh` and its seed copy, `scripts/product-init/reserved-slugs.txt`, `scripts/product-init/product-init_test.sh`, `.github/scripts/run-integration-tests.sh`) match this pin. `.gitignore` matches the pin outside the marker section, which `scripts/ci/apply-product-gitignore.sh` fills from `.gitignore.product` (`/app` and `.vscode/`). On convention paths ([CR-29](cr-rounds/CR-29-upg9-pin-72785f27.md)): `product/reserved-slugs.txt` refuses `metaldogs`; `product/checks/metaldogs-reserved-slug.sh` checks that refusal; `docker-compose.override.yml` publishes Postgres on `127.0.0.1:${SHOPANDA_DATABASE_HOST_PORT:-5432}`; `.env.example.product` and `configs/config.example.product.yaml` hold the product example lines and nothing loads them; `product/checks/metaldogs-docs.sh` runs the hardened link check when `make docs-policy` runs the hook. There is no `Makefile.product`. `.gitattributes` sets `merge=ours` on the owned and companion rows and does nothing until `git config merge.ours.driver true`. The deviation check reads `docs/PRODUCT_OWNED.md` and lists only paths the manifest does not exempt. The pre-merge snapshot directory is removed. UPG4 ticket 03 deleted the kept copies of `BoundQueue`, sync `request_id` reinjection, the jobs attempt fence, and `Admin.RequeueLease`; those match this pin. The generate claim fence is `Host.OnRequeued` in `Plugin.Init` (`pluginsdk.RequeueTx` in the re-pend transaction). Non-postgres `queue.driver` fails Init via `pluginsdk.RequireQueueDriver` (`Host.PlatformConfig`; empty counts as postgres). Terminal re-pend is `Queue.Requeue(..., WithRequeueTerminal())` on the bound queue; `OnRequeued` runs in that transaction ([overview](architecture/overview.md)). Typed MetalDogs config and empty `SHOPANDA_PLUGINS_*` overlays are fixed upstream (PR-1570/1571). `CapCatalogSeed` is CLI-only (PR-1572). It binds when the command `Needs` it, or when `Needs` is empty and `RequiresDB` is true (compat over-bind, PR-1574). A `RequiresDB: false` sibling runs without that port (PR-1590/1591). `cmd/api/cli_plugins.go` matches this pin. `pluginsdk.WithJobID` is on this pin (PR-1592). Terminal Conflict requeue retries once on `WrongAttempt` and treats `NotTerminal` as success (PR-1595, shopanda#646). Metal Dogs does not call it. `pluginsdk.JobsPort.CountsByType` is on this pin (PR-1593, shopanda#644) and Metal Dogs does not call it; health still counts through the bound queue reader. Plugin CLI assert skips `CapJobs` and `CapMedia` (PR-1594, shopanda#645); `cmd/api/plugin_boot.go` matches this pin. `internal/infrastructure/postgres/job_queue.go` matches this pin (non-UUID requeue ids are `ErrRequeueNotFound`, PR-1579). `internal/infrastructure/localfs/storage.go` matches this pin (`ReadableStorage.Read(ctx, path)`, PR-1580); the product `ReadContext` patch is gone. `plugins/metaldogs/adapters/mediastore` calls that `Read`. Listed under [`architecture/overview.md`](architecture/overview.md) and checked by `.github/scripts/check-upstream-deviations.sh`. `pkg/pluginsdk` matches this pin. `pluginsdk.WithClientCode`, `WithDetails`, `UnavailableError`, and `IsUnavailable` are on this pin (PR-1597, shopanda#650); Metal Dogs does not call them. SSR checkout confirms a Stripe PaymentIntent (`themes/default/templates/checkout_confirm.html`, `payment.stripe.publishable_key`, `GET /checkout/payment/return`; PR-1598, shopanda#651). Those theme files match this pin. `templates/emails/order_confirmed.html`, `themes/default/templates/account_order_detail.html`, and `themes/default/templates/checkout_confirm.html` match this pin; the product does not override them. Public `order_item` values are on those lines (PR-1603, shopanda#660). With the plugin enabled, `metaldogs.design_id` (public `cart_item` field, label `Design ID`) is copied onto the order line and rendered as `Design ID: <uuid>` in that email and on both order pages, because the upstream filter is visibility-only. This pin accepts that rendering. Changing the field's visibility or label is a later slice. The bump is not behaviour-neutral for the shopper. There is no enrich hook for a derived image URL. E6b does not call `OrderThumbnailURL` ([CR-38](cr-rounds/CR-38-upg12-pin-084d1206.md)). `GET /checkout/payment/return` is out-of-scope in `PublicProductVisibilityRoutes` (PR-1599, shopanda#653). No Stripe test-mode run is recorded ([CR-31](cr-rounds/CR-31-upg10-pin-c25f3e7e.md)). `OrderPort` on this pin is `FindByID`, `Lines`, `LinesForOrders`, and `ListCreatedSince` (PR-1600, shopanda#656). Core migration `112_orders_created_at_id_asc_index.sql` is the ASC keyset index for that list. Product migrations stay `plugins/metaldogs/migrations/001`–`015` (`plugin:metaldogs:<basename>`), so they do not share that core version. `finishCreatedOrder` snapshots cart extensions, then publishes `order.created` (PR-1601, shopanda#657). A snapshot error still publishes, then returns the error. `Workflow.Execute` retries `create_order` once when `created_order_id` is set and `order_created_published` is not. `pluginsdk.WithEventJobID` is on this pin (PR-1602, shopanda#658). With the flag on, the snapshot source is `Host.Orders()`. `dispatch_snapshot_order` calls `WithEventJobID` (UUID v5 of the job type and the order id). A blank id fails enqueue in `NewSyncJobEnvelopeWithID`. A repeated `order.created` is one dispatcher job. `snapshot_order_test.go` counts dispatcher jobs on the flattened `order_id` ([CR-33](cr-rounds/CR-33-upg11-pin-e062d4d3.md)). E4b binds `hostorders` when the flag is on ([CR-34](cr-rounds/CR-34-e4b-order-source.md)). The flag stays false. `pluginsdk.Host` `Cart()` and `Catalog()` are what Metal Dogs add-to-cart uses ([CR-20](cr-rounds/CR-20-mig1-cart-pluginsdk.md)). `cmd/api/metaldogs_cart.go` and `cmd/api/metaldogs_catalog.go` are removed. `metaldogs:catalog-seed` calls `Host.CatalogSeed().Import` once, with `CatalogPriceSeed.Update`. Attribute mismatch errors include the stored and requested values (PR-1573).
