# Architecture overview

Metal Dogs is a Shopanda deployment plus one external plugin (`plugins/metaldogs`) and one static storefront application (the customizer). Shopanda owns commerce; the plugin owns the design-to-artwork pipeline. `plugins/metaldogs.Plugin` implements `pluginsdk.Plugin` (`Init(pluginsdk.Host)`, `pluginsdk.Migrator`). `cmd/api/register_products.go` registers `pluginsdk.Adapt(metaldogs.New())`. See [ADR-0001](../adr/0001-metal-dogs-as-shopanda-external-plugin.md), [ADR-0002](../adr/0002-storefront-customizer-and-shopanda-checkout.md), and [CR-26](../cr-rounds/CR-26-mig3b-pluginsdk-group-b2.md).

## System context

```mermaid
flowchart LR
    customer([Dog owner on mobile or desktop])
    operator([Store operator])

    subgraph md[Metal Dogs deployment]
        shop[Shopanda + metaldogs plugin]
    end

    ai[(External image-generation API)]
    pod[(Print-on-demand provider)]
    pay[(Stripe)]
    s3[(S3-compatible object storage)]
    pg[(PostgreSQL)]

    customer -- "customize, cart, checkout" --> shop
    operator -- "admin, CLI" --> shop
    shop -- "photo + pattern + prompt" --> ai
    shop -- "order + production artwork" --> pod
    pod -- "status webhooks" --> shop
    shop -- "payment session" --> pay
    pay -- "payment webhooks" --> shop
    shop --- s3
    shop --- pg
```

## Containers / processes

One binary, three runtime modes. All share PostgreSQL and object storage.

```mermaid
flowchart TB
    subgraph browser[Browser]
        cust[Customizer app · HTML/CSS/vanilla JS]
        ssr[Shopanda SSR pages · PDP, cart, checkout, account]
    end

    subgraph serve["app serve (HTTP)"]
        core[Shopanda core: catalog, cart, checkout, orders, payments]
        mdhttp[metaldogs HTTP adapter · /api/customizer/*]
        hooks[metaldogs cart hook · checkout step · event handlers]
    end

    subgraph worker["app worker"]
        gen[integration.sync.metaldogs.generate]
        prod[integration.sync.metaldogs.prepare_production_artwork]
        snap[integration.sync.metaldogs.snapshot_order_design]
        ful[integration.sync.metaldogs.submit_fulfillment]
    end

    subgraph scheduler["app scheduler"]
        ret[metaldogs retention sweep]
        rec[metaldogs reconciliation sweep]
    end

    pg[(PostgreSQL · core tables + metaldogs_* tables + job queue)]
    s3[(Private object storage)]
    ai[(AI provider)]
    pod[(POD provider)]

    cust -- REST --> mdhttp
    cust -- "add to cart → redirect" --> ssr
    ssr --> core
    mdhttp --> pg
    mdhttp -- enqueue --> pg
    core --> hooks
    hooks --> pg
    worker --> pg
    scheduler --> pg
    gen --> ai
    gen --> s3
    prod --> s3
    ful --> pod
    ful --> s3
    mdhttp -- signed URLs --> s3
```

## Module layout inside the plugin

Organised by business capability; each module has its own domain, application, ports, and adapters. Dependencies point inward. The plugin consumes Shopanda through `plugin.Bootstrap` ports only and never imports `internal/infrastructure` or `internal/interfaces`.

```mermaid
flowchart TB
    subgraph plugin[plugins/metaldogs]
        direction TB
        subgraph customizer[customizer]
            c_http[http adapter]
            c_app[application: CreateDesign, Generate, Approve, GetDesign, ListTemplates, Upload]
            c_dom[domain: Template, Design, DesignVersion, GenerationJob, SourceAsset, policies]
            c_ports[ports: DesignRepo, TemplateRepo, AssetStore, JobQueue, Generator, Clock, IDs]
            c_adp[adapters: postgres repos, queue, storage]
            c_http --> c_app --> c_dom
            c_app --> c_ports
            c_adp -.implements.-> c_ports
        end
        subgraph generation[generation]
            g_app[application: RunGeneration, ValidateOutput, ComposeMockup, PrepareProductionArtwork]
            g_dom[domain: GenerationRequest, PromptVersion, ValidationResult, PrintSpec]
            g_ports[ports: ImageProvider, ImageOps]
            g_adp[adapters: fake provider, real provider, image ops]
            g_app --> g_dom
            g_app --> g_ports
            g_adp -.implements.-> g_ports
        end
        subgraph commerce[commerce integration]
            e_hooks[cart hook · checkout step · order.created handler]
            e_app[application: AttachDesignToCart, ValidateCheckout, SnapshotOrderDesign]
            e_dom[domain: OrderDesignSnapshot]
            e_hooks --> e_app --> e_dom
        end
        subgraph fulfillment[fulfillment]
            p_app[application: SubmitFulfillment, SyncStatus, Cancel]
            p_dom[domain: FulfillmentRecord, statuses]
            p_ports[port: FulfillmentProvider]
            p_adp[adapters: fake, real POD]
            p_app --> p_dom
            p_app --> p_ports
            p_adp -.implements.-> p_ports
        end
        c_app -- "Generator port" --> g_app
        e_app -- "public API: GetApprovedVersion, MarkOrdered" --> c_app
        p_app -- "public API: GetSnapshot" --> e_app
    end

    shopanda[Shopanda plugin.Bootstrap: DB, Queue, Storage, Events, Cart, Orders, Permissions, Config]
    c_adp --> shopanda
    e_hooks --> shopanda
    p_app --> shopanda
```

Rules (from `AGENTS.md`):

- Domain packages import nothing outside the standard library and sibling domain packages in the same module.
- Cross-module writes go through the owning module's application API (`commerce` asks `customizer` to mark a design `ordered`; it does not write `metaldogs_designs`).
- Provider payloads, DB rows, and transport DTOs stop at the adapter.

## Storefront placement

```mermaid
flowchart LR
    subgraph origin[shop origin · same host]
        customize["/customize · static customizer app (plugin public route or reverse proxy)"]
        api["/api/customizer/* · plugin HTTP"]
        ssr["/cart, /checkout, /order/... · Shopanda SSR"]
    end
    customize --> api
    customize -- "POST add-to-cart, then redirect" --> ssr
```

Serving the customizer from the shop origin avoids CORS and third-party-cookie issues with the guest cart session. Whether it is served by a plugin public route or by the reverse proxy is decided in `S1`.

## Where things live (target)

```text
/                         Shopanda fork, pinned by commit (ADR-0001, UPSTREAM_PIN.md)
├── cmd/api/register_products.go  registerMetalDogs when plugins.metaldogs.enabled  # UPG4-01
├── plugins/metaldogs/            Go plugin, hexagonal per module (see above)
│   ├── customizer/
│   ├── generation/
│   ├── commerce/
│   ├── fulfillment/
│   ├── migrations/
│   └── README.md
├── storefront/                   customizer app: HTML, CSS, vanilla JS, assets, tests
│   └── README.md
└── docs/                         Metal Dogs product docs (upstream Shopanda docs not vendored)
```

## Upstream deviations

`.github/scripts/check-upstream-deviations.sh` diffs the whole tree against the pinned SHA and allowlists every path below. It ignores product-owned trees: `plugins/metaldogs/**`, `docs/**`, `*.md` (root `README.md`, `AGENTS.md`, `RUNBOOK.md`, `PLUGINS.md`, demo `plugins/*/README.md`), `.github/workflows/**` (`ci.yml` adds `integration-metaldogs` and the hardened docs-policy step; `release.yml` refuses image name `shopanda`), and `.scratch/**`. Those trees are the product. They are not rows in the allowlist. `internal/infrastructure/localfs/storage.go` matches the pin (PR-1580); it is not listed. `internal/interfaces/http/admin/search_admin_test.go`, `internal/interfaces/http/storefront/auth_middleware_test.go`, and `cmd/api/migrate_plugins_test.go` match the pin. The customizer opaque-bearer guest check and the Metal Dogs migrator collect test live in `cmd/api/register_plugins_metaldogs_test.go`. Nothing under `pkg/` differs.

| Path | Why it differs | Introduced |
| --- | --- | --- |
| `.cursor/rules/pr-documentation-checklist.mdc` | Metal Dogs main-only / CR-round checklist (not Shopanda `dev` + `docs/phase-*`). Not a product-owned hook | F0-fix |
| `.env.example.product` | `./var/media`, Metal Dogs plugin flag, local compose DSN comment. Companion: copy into `.env`; no loader merge | UPG9-t00 |
| `.gitattributes` | `merge=ours` for owned and companion rows in `docs/PRODUCT_OWNED.md`. No effect until `git config merge.ours.driver true` | UPG9-t00 |
| `.github/scripts/check-product-docs-links.sh` | Scan Metal Dogs docs; forbid relative guide/diagram file hrefs; previous-pin full SHA guard. Not a product-owned hook | F0-fix |
| `.github/scripts/check-upstream-deviations.sh` | This allowlist | F0-fix / UPG4-05 |
| `.github/scripts/run-integration-tests.sh` | Shopanda postgres/migrate/b2b DSN suite only; Metal Dogs canaries are a separate script. Not a product-owned hook | F2 |
| `.github/scripts/run-metaldogs-integration-tests.sh` | Metal Dogs DSN suite + canaries | F2 |
| `.gitignore` | Marker section equals `.gitignore.product` (`/app`, `.vscode/`, F4 comment with no path). The rest matches the pin | UPG9-t00 |
| `.gitignore.product` | `/app`, `.vscode/`, F4 AssetStore comment. `!coverage.go` is not restored (`coverage.*` already ignores that name; no tracked `coverage.go`) | UPG9-t00 |
| `cmd/api/register_products.go` | `registerMetalDogs` via `pluginsdk.Adapt` when the plugin is enabled or the flag does not parse. `register_plugins.go` matches the pin | UPG4-01 / MIG3b-03 |
| `cmd/api/register_plugins_metaldogs_test.go` | Registration, NoCache, boot helpers, catalog-seed dispatch through `bindPluginCLIPorts`, opaque bearer guest check, Metal Dogs migrator collected when the plugin is enabled | UPG4-01 / 05 |
| `configs/config.example.product.yaml` | Commented `plugins.metaldogs` block. Companion: copy into `config.yaml`; no loader merge | UPG9-t00 |
| `docker-compose.override.yml` | Publish Postgres on `127.0.0.1:${SHOPANDA_DATABASE_HOST_PORT:-5432}`. Compose loads this beside the pin file | UPG9-t00 |
| `product/checks/metaldogs-docs.sh` | Hardened product-docs links and this allowlist. `scripts/ci/docs-policy.sh` runs `product/checks/*.sh` | UPG9-t00 |
| `product/checks/metaldogs-reserved-slug.sh` | `product-init --slug metaldogs --force` exits 1 and leaves `plugins/metaldogs` unchanged | UPG9-t00 |
| `product/reserved-slugs.txt` | `metaldogs`. `scripts/product-init.sh` refuses that slug, including `--force` | UPG9-t00 |
| `storefront/.gitkeep` | Empty storefront directory (product app is not the upstream theme tree) | F0 |

**Pin absorb honesty:** Pin is `72785f27` (PR-1596 product-owned paths, shopanda#648, on top of `d9ccc21c`). Tree equals `upstream/dev` tip `72785f27` and `upstream/main` tip `f0bf700f` (shopanda#649). Hook and example files that differed on `d9ccc21c` match this pin, except `.gitignore`, whose marker section is the product fragment. Pre-merge product text is in [`../cr-rounds/upg9-t00-preserved/`](../cr-rounds/upg9-t00-preserved/) ([CR-29](../cr-rounds/CR-29-upg9-pin-72785f27.md)): `.env.example`, `.gitignore`, `Makefile`, `configs/config.example.yaml`, `docker-compose.yml`, `scripts/ci/docs-policy.sh`, its seed copy, `scripts/product-init/reserved-slugs.txt`, `scripts/product-init/product-init_test.sh`. Review iteration 1 reapplied the slug refuse, the host port, and the example lines on the owned and companion rows in the table. Review iteration 2 reapplied `/app` and `.vscode/` through `.gitignore.product` and guards the slug refuse in `product/checks/metaldogs-reserved-slug.sh`. The F4 AssetStore gitignore line is a comment with no path, so `./var/media` stays unignored, same as before the bump. `!coverage.go` is not restored. Companions are not merged by a loader. `merge=ours` needs `git config merge.ours.driver true` or git text-merges those paths. The checker still allowlists those rows; ticket 01 should drop them when the checker reads `docs/PRODUCT_OWNED.md`, and should delete the snapshot directory after the remaining hunks (Makefile recipe, docs-policy body and seed) move. Do not copy the metaldogs hunk back into `scripts/product-init/product-init_test.sh`. `.github/scripts/*` and `.cursor/rules/*` stay on this list (upstream does not cover them). Metal Dogs admin health calls `Host.Jobs().CountsByType`. Generate and prepare enqueue call `SyncJobs.Enqueue` with `WithJobID` (UUID). Stale generate claims are requeued by the plugin after that enqueue. `cmd/api/plugin_boot.go` matches the pin. Cart extension fakes are race-free; `scripts/ci/unit.sh` runs `go test -race ./internal/application/cart/`. `ReadableStorage.Read` takes a context; `localfs/storage.go` matches the pin. Non-UUID job ids are `ErrRequeueNotFound` (PR-1579). `internal/` and `pkg/` have no Metal Dogs edits. `CapCatalogSeed` is CLI-only. It binds on a command that `Needs` it, or on empty `Needs` with `RequiresDB` (compat over-bind, PR-1574). `RequiresDB: false` siblings are not blocked (PR-1590/1591). The generate claim fence is `Host.OnRequeued`. Terminal re-pend is `Queue.Requeue` with `WithRequeueTerminal`. Add-to-cart uses `pending_line`, `ClassifyProductSlug`, and `DiscardEmptyGuestCart` ([CR-21](../cr-rounds/CR-21-upg4-pin-7c982b00.md)). E1 catalog seed uses `Host.CatalogSeed().Import`. Treat the table as the conflict allowlist together with [`../runbooks/shopanda-upgrade.md`](../runbooks/shopanda-upgrade.md).

Architecture diagrams in this directory describe the **target** module layout and job names (`integration.sync.metaldogs.*`). F3 adds platform job observability + a thin customizer health/obs.ping surface. G0 adds offline `metaldogs:eval` (fake provider).
