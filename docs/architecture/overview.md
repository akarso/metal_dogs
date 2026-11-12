# Architecture overview

Metal Dogs is a Shopanda deployment plus one external plugin (`plugins/metaldogs`) and one static storefront application (the customizer). Shopanda owns commerce; the plugin owns the design-to-artwork pipeline. See [ADR-0001](../adr/0001-metal-dogs-as-shopanda-external-plugin.md) and [ADR-0002](../adr/0002-storefront-customizer-and-shopanda-checkout.md).

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
├── cmd/api/register_plugins.go   registerMetalDogs when the plugin is enabled  # F1 / E1
├── cmd/api/metaldogs_catalog.go  core catalog repos for metaldogs:catalog-seed  # E1
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

Files that differ from the pinned Shopanda commit outside `plugins/metaldogs/` and `storefront/` must be listed here and re-checked on every upgrade. Treat this table as the upgrade conflict allowlist together with [`../runbooks/shopanda-upgrade.md`](../runbooks/shopanda-upgrade.md).

| Path | Why it differs | Introduced |
| --- | --- | --- |
| `README.md` | Metal Dogs banner, remotes, badges → this repo; documentation section points at Metal Dogs docs + pinned upstream guides | F0 / F0-fix |
| `AGENTS.md` | Metal Dogs agent rules, remotes, **main-only** branch policy, build commands (replaces upstream AGENTS.md / phase-* workflow) | F0 / F0-fix |
| `.gitignore` | Union of upstream ignores with Metal Dogs IDE/binary entries (`/app`, `.idea/`) | F0 |
| `RUNBOOK.md` | Metal Dogs runbook index; Shopanda operator detail via pinned GitHub URLs (full upstream RUNBOOK not retained) | F0-fix |
| `PLUGINS.md` | Guide/diagram hrefs rewritten to pinned upstream blob URLs; COMMERCIAL stays local | F0-fix |
| `plugins/*/README.md` (demos + b2b) | Guide hrefs/backticks rewritten to pinned upstream blob URLs (including fragment links) | F0-fix |
| `.github/scripts/check-product-docs-links.sh` | Skip missing paths; scan Metal Dogs docs; forbid relative/`../` guide/diagram **file** hrefs (incl. `#fragment` / `?query`) and backtick paths; inline self-test before scan | F0-fix |
| `.cursor/rules/pr-documentation-checklist.mdc` | Metal Dogs main-only / CR-round checklist (not Shopanda `dev` + `docs/phase-*`) | F0-fix |
| `docs/**` (product) | Metal Dogs roadmap/ADRs/architecture/runbooks/cr-rounds | F0 |
| `docs/GOVULN_BASELINE.md` | Restored for fail-closed `govuln` CI (empty allowlist at pin) | F0-fix |
| `docs/COMMERCIAL.md` | Restored (B2B licensing boundary inherited with the GPL tree) | F0-fix |
| `docs/guides/README.md`, `docs/diagrams/README.md` | Stubs pointing at pinned upstream trees (full guides/diagrams not vendored) | F0-fix |

Inherited CI workflows still mention Shopanda `main`/`dev` dual-branch semantics; Metal Dogs ignores promote tooling until an upgrade deliberately adapts it.

| Path | Why it differs | Introduced |
| --- | --- | --- |
| `cmd/api/register_plugins.go` | Calls `registerMetalDogs` when `plugins.metaldogs.enabled` | F1 / E1 |
| `cmd/api/metaldogs_catalog.go` | Builds core product, variant, price, price-history and stock repos for `metaldogs:catalog-seed`. The plugin package does not import infrastructure adapters | E1 |
| `internal/platform/config/config.go` (+ test) | `Plugins.MetalDogs` / `SHOPANDA_PLUGINS_METALDOGS_ENABLED` (default false) | F1 |
| `configs/config.example.yaml` | Documented `plugins.metaldogs` toggle | F1 |
| `plugins/metaldogs/**` | Product plugin (hexagonal layout + Migrator; F1 `/enabled`, F3 health/obs — see F3 rows) | F1 |
| `cmd/api/cache_control_prefixes.go` (+ test), `cmd/api/wire_routes.go` | Shared `cacheControlNoCachePrefixes()` includes `/api/customizer`; `wire_routes` calls the helper instead of an inline list | F1-fix |
| `docker-compose.yml` | Publish Postgres on `127.0.0.1:${SHOPANDA_DATABASE_HOST_PORT:-5432}` for host-side tests | F2 |
| `.github/scripts/run-metaldogs-integration-tests.sh` | Metal Dogs–only DSN suite + canaries (decoupled from Shopanda) | F2 |
| `.github/workflows/ci.yml` | `integration-metaldogs` job + no-DSN / metaldogs coverage artifacts; docs-policy uses MD hardened checker (no promote step) | F2 / CR-06 |
| `.github/workflows/release.yml` | Fail-closed unless `vars.SHOPANDA_IMAGE_NAME` is set and ≠ `shopanda` | CR-06 |
| `Makefile` `docs-policy` | Runs `.github/scripts/check-product-docs-links.sh` (not Phase 17 scanner / not `SHOPANDA_PLATFORM=1`) | CR-06 |
| `scripts/product-init/reserved-slugs.txt` | Includes `metaldogs` so `--force` cannot clobber the product plugin | CR-06 |
| `internal/platform/plugin/providers.go` (+ test) | `App.BoundQueue()` — prefer `BindQueue` over `RegisterQueue` for health/read paths | F3 residual (not in upstream #568) |
| `internal/application/integration/sync_runtime.go` (+ test) | Sync handler re-injects payload `request_id` into ctx when worker ctx has none | F3 residual (not in upstream #568) |
| `cmd/api/job_queue_bind.go`, `cmd/api/jobs_cli.go`, `cmd/api/bind_queue_test.go` | Enabled plugin installs the generate claim fence on the bound queue (HTTP and `jobs:retry`) | C4 |
| `cmd/api/main.go` | `jobs:retry` help includes reclaim of stale processing | C4 |
| `internal/domain/jobs/job.go`, `worker.go` (+ `worker_test.go`) | Delivery fence: `Complete`/`Fail` take the dequeued attempt; `ErrSkipOutcome` does not write a platform outcome | C4 |
| `internal/domain/jobs/admin.go` | `Requeue`, and `Retry` reclaims processing older than `DefaultStaleProcessingLease` | C4 |
| `internal/application/integration/sync_manual_roundtrip_test.go`, `internal/platform/plugin/sync_job_test.go`, `internal/platform/runtime/scheduler_test.go` | Call sites follow the attempts fence | C4 |
| `cmd/api/register_plugins_metaldogs_test.go`, `migrate_plugins_test.go`, `config_test.go` MetalDogs cases | Product registration / migrate / config coverage | F1+ |
| `plugins/metaldogs/**` (correlation alias, public/admin health, `obs.ping`) | Product facade; depths admin-only. Depends on residual `BoundQueue` + sync reinjection above | F3 |
| `plugins/metaldogs` AssetStore (`ports`, `adapters/mediastore` → platform SignedURLStorage) | Private object storage port; keys under `private/blobs/`; C2 `UploadService` Put + `metaldogs_assets` | F4 / C2 |
| `plugins/metaldogs/generation/**`, `cli.go` (`metaldogs:eval`) | Offline ImageProvider port + fake + eval report; CLI only (no HTTP) | G0 |
| `plugins/metaldogs/generation/openai`, spike templates, `ocr` | OpenAI Images candidate + scorecard/OCR stub; smoke gated | G1 |

**Pin absorb honesty:** #568 / #570 / #571 platform merges landed at `a8cbcb18` and remain at pin `43f0a1ed`. F3 still carries Metal Dogs–only platform deltas (`BoundQueue`, sync `request_id`→ctx). C4 still carries the generate claim fence and the attempts delivery fence on `jobs.Queue`. The #573 delta did not touch those files. E1 adds `cmd/api/metaldogs_catalog.go`: core catalog repositories for `metaldogs:catalog-seed`, injected from `cmd/api` so the plugin does not import infrastructure adapters. Treat those rows as required conflict allowlist entries until upstreamed or the MD facade is rewritten not to need them.

Architecture diagrams in this directory describe the **target** module layout and job names (`integration.sync.metaldogs.*`). F3 adds platform job observability + a thin customizer health/obs.ping surface. G0 adds offline `metaldogs:eval` (fake provider).
