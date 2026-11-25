# plugins/metaldogs

Metal Dogs Shopanda external plugin. `Plugin` implements `pluginsdk.Plugin`: `Init(pluginsdk.Host)`, `Needs`, and `pluginsdk.Migrator` (`Migration` returns `pluginsdk.MigrationSpec`). `cmd/api/register_products.go` registers `pluginsdk.Adapt(metaldogs.New())`.

## Enable

Default **off** (dark). Enable with:

```yaml
plugins:
  metaldogs:
    enabled: true
```

or `SHOPANDA_PLUGINS_METALDOGS_ENABLED=true`.

Registration: `cmd/api/register_products.go` (`registerProductPlugins`, compile-time). Enabling also registers the plugin `Migrator` (`plugin:metaldogs:*` versions) for `app migrate` / boot.

## Layout

```text
plugins/metaldogs/
├── plugin.go              # pluginsdk.Plugin: Init(Host), Needs, AssetStore wrapper
├── cli.go                 # metaldogs:eval, templates, catalog-seed
├── catalog/               # products.csv, prices.csv, stock.csv + seed loader
├── assets.go              # wrap Bound/registered SignedURLStorage
├── sync_obs.go            # obs.ping RegisterManual
├── sync_generate.go       # generate RegisterManual (C4)
├── sync_prepare_production.go  # prepare_production_artwork (pending_spec until production_spec is set)
├── migrations.go          # pluginsdk.Migrator + MigrationFS
├── migrations/            # SQL (namespaced plugin:metaldogs:*)
├── generation/            # ImageProvider port + fake (ADR-0008 / G0)
│   ├── eval/              # offline runner + report.json
│   ├── validate/          # G3 output checks
│   └── mockup/            # G4 Compose + embedded 80×100 placeholder tee
├── httpapi/               # enabled + health + templates + uploads + designs + generate + approve/clone + add-to-cart
├── observability/         # correlation_id alias helpers
├── domain/                # Template, Asset, Design, DesignVersion, GenerationJob, …
├── application/           # TemplateCatalog, UploadService, DesignService, Generate*
├── ports/                 # Clock, IDGenerator, MetaStore, AssetStore, repos
├── adapters/postgres/     # TemplateStore, AssetRepo, DesignRepo, GenerationJobRepo, DesignVersionRepo
├── adapters/mediastore/   # wrap media.SignedURLStorage (+ ReadableStorage Read)
└── testutil/              # OpenIsolated, FakeClock, NewSeqGenerator (fakes only)
```

## Current surface

| Method | Path | Auth | When enabled |
| --- | --- | --- | --- |
| `GET` | `/api/customizer/enabled` | public | `{"status":"ok","plugin":"metaldogs"}` |
| `GET` | `/api/customizer/health` | public | `Host.Jobs()` bound (`CapJobs`); generic `degraded` reason; HTTP 503 when degraded |
| `GET` | `/api/customizer/templates` | public | Published catalog (`?genre=`); DTO without prompts/model; `thumbnail_url` null until asset ingest |
| `GET` | `/api/customizer/templates/{id}` | public | Published UUID only; draft/unknown → `404` |
| `POST` | `/api/customizer/uploads` | public | Multipart `file` → oriented JPEG/PNG source + signed preview; media+DB; concurrency-capped |
| `POST` | `/api/customizer/designs` | public | Create draft + `owner_token` once; binds upload |
| `GET` | `/api/customizer/designs/{id}` | Bearer | Design DTO; wrong token → `404` |
| `PATCH` | `/api/customizer/designs/{id}` | Bearer | Texts / replace photo (editable statuses only) |
| `POST` | `/api/customizer/designs/{id}/generate` | Bearer + `Idempotency-Key` | `202` GenerationJob; enqueues sync generate |
| `GET` | `/api/customizer/generation-jobs/{id}` | Bearer | Job status + `retry_after` |
| `POST` | `/api/customizer/designs/{id}/approve` | Bearer | Freeze `DesignVersion`; design → `approved`; enqueue prepare (no spec: `pending_spec`, version stays `pending`; spec set: store the file or `failed` + `production_failure_reason`; enqueue failure is 503) |
| `POST` | `/api/customizer/designs/{id}/clone` | Bearer | New draft + token; Put-copies source bytes |
| `POST` | `/api/customizer/designs/{id}/add-to-cart` | Bearer | Session cart line for one custom-tee variant; `200` `{ "cart_url": "/cart" }`. `metaldogs.design_id` is private. Contract: [`docs/architecture/api.md`](../../docs/architecture/api.md) |
| `GET` | `/api/v1/admin/customizer/health` | `jobs.read` | Per-type depths via `Host.Jobs().CountsByType` |
| `GET` | `/api/v1/admin/orders/{order_id}/artwork` | `metaldogs.snapshots.read` (`admin` only) | Snapshot metadata and 300s signed URLs. Only when `order_snapshot.enabled` is true. Contract: [`docs/architecture/api.md`](../../docs/architecture/api.md) |
| `GET` | `/api/customizer/order-thumbnails/{token}` | public | Thumbnail bytes for one order line. Only when `order_snapshot.enabled` is true and `thumbnail_signing_key` is set. Not `NoCache`. Contract: [`docs/architecture/api.md`](../../docs/architecture/api.md) |

**Not k8s probes.** Use `/healthz` and `/readyz`. Guest `/api/customizer/*` responses are `Cache-Control: no-store`, except a `200` from `GET /api/customizer/order-thumbnails/{token}` (`private, max-age=86400`). That route's `404` is `no-store`. Depths stay admin-only (backlog/failure recon). Contract: [`docs/architecture/api.md`](../../docs/architecture/api.md).

## Sale check (E2 cart hook, E3 checkout)

`validateDesignLine` (`design_line.go`) is the only sale rule. `cart.validate` and checkout step `metaldogs_design_sale` both call it. The step is registered at `before:reserve_inventory` only when the plugin is enabled. It loads the cart through `Host.Cart()`, uses the plugin clock, and classifies lines the same way as the hook (custom tee slug `mdogs-tee`). `approved` or `ordered`, and not expired, passes. A loaded product with another slug is ignored. An unknown product fails the step. A cart with no custom-tee line is left alone.

A refusal is `pluginsdk.ValidationError` with the message `Design no longer available`, code `plugin.metaldogs.design_unavailable`, and `details.lines` (`variant_id`, granular code) plus `retryable: false`. A cart, catalog, or design-store read failure stops checkout as `503` `plugin.metaldogs.validation_unavailable` with `retryable: true`. The cause stays in the log. The add-to-cart route still accepts only `approved`. An `ordered` design is `409` `invalid_transition`; re-order is `POST …/clone`. Contract: [`docs/architecture/api.md`](../../docs/architecture/api.md). SSR confirm exists upstream (PR-1598). Guest Stripe test mode is a maintainer checklist, not a recorded result: [`docs/runbooks/checkout-manual-verification.md`](../../docs/runbooks/checkout-manual-verification.md).

## Order snapshot (E4a / E4b)

`plugins.metaldogs.order_snapshot.enabled` defaults to false (`order_snapshot.enabled`, or `SHOPANDA_PLUGINS_METALDOGS_ORDER_SNAPSHOT_ENABLED`). While it is false, `snapshot_order_design`, `dispatch_snapshot_order`, and `reconcile_snapshots` are not registered, no `OrderSource` is bound, and `./app metaldogs:snapshots reconcile --fix` is refused. `./app metaldogs:snapshots reconcile` and `--order` exit `order source is not bound` and print nothing. When the flag is true, `Init` calls `Require(CapOrders)` and binds `adapters/hostorders` from `Host.Orders()`. A paid custom-tee checkout then enqueues the dispatcher and the snapshot job. A root that asserts capabilities fails boot when that port is unbound (no skip list omits `CapOrders`) (`required capability "orders" not bound`) before those jobs run. A root that does not assert capabilities (tests, a custom root) leaves the source nil: `reconcile_snapshots` fails the run, logs `metaldogs.reconcile_snapshots.failed`, and emits no `metaldogs.reconcile_snapshots.gap` line. An alert on that gap line is not configured. The report (`order_id`, `line_key` as `order_id/variant_id`, `reason`) prints only when a source is bound. E4a tests call `SetOrderSource` with an in-memory fake. Runbook: [`docs/runbooks/order-without-design-snapshot.md`](../../docs/runbooks/order-without-design-snapshot.md). Rounds: [CR-32](../../docs/cr-rounds/CR-32-e4a-order-design-snapshot.md), [CR-34](../../docs/cr-rounds/CR-34-e4b-order-source.md).

## Order artwork (E5)

Same flag. While `order_snapshot.enabled` is false, `GET /api/v1/admin/orders/{order_id}/artwork`, permission `metaldogs.snapshots.read`, and `./app metaldogs:order-artwork` are not registered. When it is true, both surfaces call `application.OrderArtworkAccess`. TTL is 300 seconds. `production` is null when the snapshot line has no production asset. Each issued URL is one append-only row in `metaldogs_artwork_access_log` (migration `016`): order id, variant id, kind `approved` or `production`, actor, channel `admin` or `cli`, `requested_at`. The row has no URL and no storage key. A failed audit write returns no URL. HTTP actor is `pluginsdk.CustomerID` (the admin user id). CLI actor is the literal `cli` and does not check the permission. Runbook: [`docs/runbooks/manual-pod-fulfillment.md`](../../docs/runbooks/manual-pod-fulfillment.md). Round: [CR-35](../../docs/cr-rounds/CR-35-e5-artwork-access.md).

## Order thumbnail (E6a)

Same flag, plus `plugins.metaldogs.thumbnail_signing_key` (no default, at least 32 characters, random secret, not a passphrase; env `SHOPANDA_PLUGINS_METALDOGS_THUMBNAIL_SIGNING_KEY`). Flag off, or flag on with the key absent: `GET /api/customizer/order-thumbnails/{token}` is not registered and `(*Plugin).OrderThumbnailURL` returns empty. A shorter key fails Init. The route is registered without `NoCache`. The handler writes the thumbnail bytes (`Content-Type` allowlist `image/jpeg`, `image/png`, `image/webp`, `Content-Disposition: inline`, `Cache-Control: private, max-age=86400` on `200`, `X-Content-Type-Options: nosniff`). Every failure is `404` `{"error":"not_found"}` with `Cache-Control: no-store`. The thumbnail asset has a null `owner_ref`. The handler does not log the token. The platform HTTP access log records the matched ServeMux pattern (`GET /api/customizer/order-thumbnails/{token}`), not the raw path (PR-1604, [CR-39](../../docs/cr-rounds/CR-39-upg13-pin-7ab17606.md)). A reverse proxy that logs the raw URL can still record the token. `OrderThumbnailURL` prefixes `plugins.metaldogs.public_base_url` (env `SHOPANDA_PLUGINS_METALDOGS_PUBLIC_BASE_URL`). The plugin cannot read `server.public_base_url`. Round: [CR-37](../../docs/cr-rounds/CR-37-e6a-order-thumbnail.md).

## Order line thumbnail (E6b)

Same flag and key, plus an https `public_base_url`. Init registers hook `order.line.display` (priority 100, same style as `cart.validate`) only when the plugin is enabled, `order_snapshot.enabled` is true, `thumbnail_signing_key` is set, and `public_base_url` is https. Otherwise the hook is absent. If the flag, the key, and a base URL are set but the base is not https, Init logs `metaldogs.order_line_display.public_base_url` once (`reason=not_https`) and does not register the hook. The thumbnail route itself stays the E6a rule (flag and key, including http).

The handler reads `order_id`, `variant_id`, and `language`, then calls `OrderThumbnailURL`. An empty URL or any error adds no field and logs `metaldogs.order_line_display.skipped` or `metaldogs.order_line_display.failed` with `order_id` and `variant_id` only. The handler returns nil. A hit appends one field: code `metaldogs.thumbnail`, kind image, value the URL. Label and alt come from one table, chosen by the BCP 47 primary subtag (`pl-PL` → pl; unknown or empty → en):

| Language | Label | Alt |
| --- | --- | --- |
| en | Your design | Preview of your custom design |
| pl | Twój projekt | Podgląd Twojego projektu |
| de | Dein Design | Vorschau deines individuellen Designs |
| fr | Votre création | Aperçu de votre création personnalisée |
| es | Tu diseño | Vista previa de tu diseño personalizado |

Shopanda renders that field on the confirmation email and on both order pages. This plugin does not edit those templates. `localhost` is not a public host, so the platform drops the image on a local origin. The lookup does not require the order to be paid. Round: [CR-42](../../docs/cr-rounds/CR-42-e6b-order-thumbnail-display.md).

Sync jobs: `integration.sync.metaldogs.obs.ping` (F3 sample); `integration.sync.metaldogs.generate` (C4); `integration.sync.metaldogs.prepare_production_artwork` (no `production_spec`: `pending_spec`, version stays `pending`; spec set: G5a stores the file, [CR-36](../../docs/cr-rounds/CR-36-g5a-production-artwork.md); approve leaves failed/done alone). Generate and prepare enqueue call `SyncJobs.Enqueue` with `WithJobID` (UUID). After a successful generate enqueue, a processing row older than `DefaultClaimLease` is requeued with `WithRequeueMinAge`; a fresh processing row is left. Prepare keeps its own staleness rule (failed and done stay; cancelled and stale processing use the type-scoped SQL reclaim). Platform injects `request_id` on Enqueue and again in the sync handler; MD logs also emit `correlation_id` (same value). The generate claim fence is `Host.OnRequeued` in `Plugin.Init`. See [`docs/UPSTREAM_PIN.md`](../../docs/UPSTREAM_PIN.md) and [CR-26](../../docs/cr-rounds/CR-26-mig3b-pluginsdk-group-b2.md).

Generation config (C4/C7) is `Config.Extra["metaldogs"]`, read with `Host.ConfigValue`. Env names are unchanged: `SHOPANDA_PLUGINS_METALDOGS_GENERATION_PROVIDER` (Extra key `generation_provider`; dotted alias `generation.provider`; fake only), `..._MAX_RETRIES`, `..._MAX_ATTEMPTS_PER_DESIGN`, `..._COOLDOWN_SECONDS` (default 30; `0` disables). A nested YAML `generation:` map (`provider`, `max_retries`, `max_attempts_per_design`, `cooldown_seconds`) loads those same fields. A non-integer is an error. `plugins.metaldogs.production_spec` has no defaults (G5a, [CR-36](../../docs/cr-rounds/CR-36-g5a-production-artwork.md)). Omit it and `prepare_production_artwork` stays on `pending_spec`. A present section with a missing or invalid field ends that plugin's Init: worker, scheduler, and the jobs CLI refuse to start; `serve` starts with metaldogs inactive (no routes and no jobs). Keys, marked example-only, are in `configs/config.example.product.yaml`.

## Private assets (F4 / C2 / ADR-0004)

- Port: `ports.AssetStore` (`Put` / `Read` / `Head` / `SignedGetURL` / `Delete`). Writers: `UploadService` (C2), generate worker artwork + mockup (C4/C5), clone source Put-copy (C6). `Read` requires `media.ReadableStorage` (local FS).
- Metadata: `metaldogs_assets` via `ports.AssetRepository` (`owner_ref` = design UUID; unique per `(owner_ref, type)` so source + artwork + mockup can share a design; unattached `delete_after=+2h`, attached source `+7d` from activity incl. generate; sweep = O2). Approved freeze: `metaldogs_design_versions` (C6 / ADR-0005).
- Designs: `POST/GET/PATCH /api/customizer/designs` with ADR-0007 Bearer owner tokens (platform JWT parse skipped on `/api/customizer/*`). After generate success, DTO may include signed `artwork_preview_url` / `mockup_preview_url` (60s TTL). Approve/clone: see [`docs/architecture/api.md`](../../docs/architecture/api.md).
- Keys: `private/blobs/metaldogs/...` via `ports.ObjectKey`; mediastore **rejects** any other key.
- Delivery: wrap core `media.SignedURLStorage` (inline signed GET; TTL capped ≤15m). No product shim.
- Local: `SHOPANDA_MEDIA_LOCAL_BASE_PATH=./var/media` (never FileServer); `SHOPANDA_MEDIA_LOCAL_SIGN_SECRET` (≥32 bytes) outside DEV.

## Test harness (F2)

```bash
export SHOPANDA_TEST_DSN='postgres://shopanda:changeme@127.0.0.1:5432/shopanda?sslmode=disable'
go test ./plugins/metaldogs/... -count=1
```

`testutil.OpenIsolated` wraps `harness.OpenIsolated`: unique schema per call, forced `search_path`, Metal Dogs plugin migrations (`OpenIsolatedWithCore` also applies core). CI: `integration-metaldogs` canaries. Plugin advisory locks stay global per plugin name, so the suite still runs with `-p 1`.

Later job types: `integration.sync.metaldogs.<name>` (ADR-0003).

## Offline eval (G0)

With the plugin enabled (`SHOPANDA_PLUGINS_METALDOGS_ENABLED=true`), `Init` registers CLI commands, customizer HTTP, and sync jobs. Media is a wrapper that calls `Host.Media()` on each use; the composition root binds storage after `InitAll`. A missing or unsigned backend fails that asset call. Route registration does not wait for storage. `AssetStore()` is non-nil after Init and does not mean media is bound.

`./app metaldogs:eval` is dispatched on the process CLI and does not open a DSN (`RequiresDB: false`). The rejection in [CR-22](../../docs/cr-rounds/CR-22-mig2-pluginsdk-group-a.md) is superseded by [CR-23](../../docs/cr-rounds/CR-23-upg5-pin-ad14bd3a.md).

```bash
./app metaldogs:eval \
  -template plugins/metaldogs/templates/head-front-blackmetal.yaml \
  -photos /path/to/photos \
  -out /tmp/metaldogs-eval \
  -provider fake \
  -dog-name Burek
```

Writes `photo.ext.png` per input (e.g. `dog.jpg.png`) plus `report.json` (`template_model` vs per-sample `provider_model`, parameters/seed; `validation.status` = `pass`/`fail`/`skipped` via `generation/validate`). Hard validation failures set sample `error` / `error_class=validation_failed` (non-zero CLI exit). `--out` must have no non-dot entries. Default provider is `fake` (deterministic monochrome fixtures). Canvas ≤2048²; photo ≤20MiB. Accepts Template v1 YAML or a legacy G0 stub JSON.

### Templates (G2)

`validate` does not query. The command is `RequiresDB`, so the process must open the configured DSN ([CR-22](../../docs/cr-rounds/CR-22-mig2-pluginsdk-group-a.md)).

```bash
./app metaldogs:templates validate -file plugins/metaldogs/templates/head-front-blackmetal.yaml
./app metaldogs:templates load -file plugins/metaldogs/templates/head-front-blackmetal.yaml   # draft
./app metaldogs:templates publish -slug head-front-blackmetal   # not done in G2 ship set
./app metaldogs:templates retire -slug head-front-blackmetal
```

Definitions live under [`templates/`](templates/). See [`docs/runbooks/template-publishing.md`](../../docs/runbooks/template-publishing.md).

### OpenAI spike (G1)

Default model `gpt-image-2.5-sunburst` (`gpt-image-1` rejected). Adapter posts `image[]` + `image/*` MIME, no `response_format`.

```bash
export METALDOGS_OPENAI_API_KEY=sk-...
./app metaldogs:eval -template plugins/metaldogs/templates/head-front-blackmetal.yaml \
  -photos /path/to/photos -out /tmp/md-spike-01 -provider openai -dog-name Burek
# optional: -openai-model … -openai-base …
# Paid smoke (not CI): METALDOGS_SMOKE=1 go test ./plugins/metaldogs/generation/openai -run Smoke -count=1
```

See [`docs/architecture/generation-quality.md`](../../docs/architecture/generation-quality.md).

## Catalog (E1)

Manual command only. Migrations and startup do not run it. The plugin must be enabled (`plugins.metaldogs.enabled` or `SHOPANDA_PLUGINS_METALDOGS_ENABLED=true`). The command needs a database. It does not need media storage.

```bash
./app metaldogs:catalog-seed -dir plugins/metaldogs/catalog
```

Data files in [`catalog/`](catalog/):

| File | Role |
| --- | --- |
| `products.csv` | One product, five size rows. Name "Metal Dogs custom tee", slug `mdogs-tee`, parent SKU `MDOGS-TEE`, type `configurable`, status `draft`. Variant SKUs `MDOGS-TEE-S` … `MDOGS-TEE-XXL`. Column `size` is the variant attribute `size`. |
| `prices.csv` | One net price per SKU, currency `PLN`, no store. Placeholder `80,49`, marked placeholder in the `note` column. |
| `stock.csv` | Quantity `0` for every SKU. |

The loader checks all three files before it writes. A bad file exits non-zero and names the file, row and column. The command then calls `Host.CatalogSeed().Import` once, in one transaction. Existing variant SKUs are not inserted again. Product `Status` is not sent, so a product that already exists keeps its status, including `active` after a later publish. A price is sent with `Update`: a different amount is written and a price-history row is added; the same amount is a no-op. Stock is set to the file quantity on every successful run. A second run with unchanged files does not insert products, variants, or prices and does not change status. The stock quantity stays the same when it already matches; the stock row's `updated_at` moves because `SetStock` upserts.

`parent_sku` is stored on the product in that same insert. Attributes are create-only. A later run does not add a missing `parent_sku` and does not change a different stored value. Either case fails the run and rolls the batch back. The error includes the stored value and the requested value (`stored=<missing>` when the attribute is absent).

Shopanda assigns the tax class from the product type. `configurable` maps to the standard class (`catalog.TaxClassCode`). This command writes no tax setting. The shop seed's standard rate for PL is 2300 basis points (23%).

### Price format

The CSV holds a human decimal. The command converts it to minor units (PLN grosz) with no rounding, then `CatalogSeedPort` stores that integer.

Accepted:

- One separator, comma or dot, is the decimal separator: `80,49` and `80.49` → `8049`. One fractional digit is padded with a trailing zero (`80,5` → `8050`).
- When both separators appear, the last one is decimal and the other groups thousands in threes: `1.234,56` and `1,234.56` → `123456`.
- Digits with no separator are whole currency units: `80` → `8000`.

Rejected (examples: `80,499`, `80 zł`, `80 PLN`, `-5`, `0`, empty, `abc`):

- More than two fractional digits.
- Spaces, currency symbols or letters.
- A negative amount, zero or an empty cell.

### Net price, gross display

Stored amounts are **net**. Confirmed in the core price pipeline before the price file was written: the price row is the line unit price, and tax runs afterwards. Cart and checkout set `tax_mode` to `tax.ModeExclusive` when the caller does not. Exclusive mode adds tax on top (`internal/domain/tax/calculate.go`: `tax = price * rate / 10000`, integer division). Inclusive mode would treat the stored amount as gross; that path is not the default. The public offer `amount` is this stored net value in minor units.

Gross is computed from the current rate and is never stored:

```text
gross_minor = net_minor + net_minor * rate / 10000
```

Placeholder `8049` at rate `2300` → tax `1851` → gross `9900` (99,00 PLN).

### Publish at M3

Draft status and zero stock are two independent protections. The storefront hides a `draft` product. Stock `0` makes `in_stock` false, so the tee stays unbuyable if it is published by mistake. Launch removes both.

1. Replace the placeholder in `prices.csv` with the real net amount and run `metaldogs:catalog-seed`. Rows that already match the file are skipped; a different amount is updated and a price-history row is written.
2. Set the launch quantity in `stock.csv` (real stock, or a high reservable quantity) and run the command again. The stock row is set to that quantity.
3. Set the Shopanda product `mdogs-tee` to status `active` in the admin catalog. Leave `products.csv` as `draft`. The loader rejects any other status, and it does not change the status of a product that already exists.

### Limits

- Product, variant, price, and stock writes share one `Import` transaction. A failure after validation rolls the batch back. Validation of the files still happens before any write.
- CSV row numbers assume one record per line.
- Leading zeros in a price are accepted (`080,49` → `8049`).
