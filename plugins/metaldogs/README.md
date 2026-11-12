# plugins/metaldogs

Metal Dogs Shopanda external plugin.

## Enable

Default **off** (dark). Enable with:

```yaml
plugins:
  metaldogs:
    enabled: true
```

or `SHOPANDA_PLUGINS_METALDOGS_ENABLED=true`.

Registration: `cmd/api/register_plugins.go` (compile-time). Enabling also registers the plugin `Migrator` (`plugin:metaldogs:*` versions) for `app migrate` / boot.

## Layout

```text
plugins/metaldogs/
├── plugin.go              # Init + Name + AssetStore wiring
├── cli.go                 # metaldogs:eval, templates, catalog-seed
├── catalog/               # products.csv, prices.csv, stock.csv + seed loader
├── assets.go              # wrap Bound/registered SignedURLStorage
├── sync_obs.go            # obs.ping RegisterManual
├── sync_generate.go       # generate RegisterManual (C4)
├── sync_prepare_production.go  # prepare_production_artwork no-op (C6 → G5)
├── migrations.go          # plugin.Migrator + MigrationFS
├── migrations/            # SQL (namespaced plugin:metaldogs:*)
├── generation/            # ImageProvider port + fake (ADR-0008 / G0)
│   ├── eval/              # offline runner + report.json
│   ├── validate/          # G3 output checks
│   └── mockup/            # G4 Compose + embedded 80×100 placeholder tee
├── httpapi/               # enabled + health + templates + uploads + designs + generate + approve/clone
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
| `GET` | `/api/customizer/health` | public | BoundQueue/Reader readiness only; generic `degraded` reason; HTTP 503 when degraded |
| `GET` | `/api/customizer/templates` | public | Published catalog (`?genre=`); DTO without prompts/model; `thumbnail_url` null until asset ingest |
| `GET` | `/api/customizer/templates/{id}` | public | Published UUID only; draft/unknown → `404` |
| `POST` | `/api/customizer/uploads` | public | Multipart `file` → oriented JPEG/PNG source + signed preview; media+DB; concurrency-capped |
| `POST` | `/api/customizer/designs` | public | Create draft + `owner_token` once; binds upload |
| `GET` | `/api/customizer/designs/{id}` | Bearer | Design DTO; wrong token → `404` |
| `PATCH` | `/api/customizer/designs/{id}` | Bearer | Texts / replace photo (editable statuses only) |
| `POST` | `/api/customizer/designs/{id}/generate` | Bearer + `Idempotency-Key` | `202` GenerationJob; enqueues sync generate |
| `GET` | `/api/customizer/generation-jobs/{id}` | Bearer | Job status + `retry_after` |
| `POST` | `/api/customizer/designs/{id}/approve` | Bearer | Freeze `DesignVersion`; design → `approved`; enqueue prepare (stub fails until G5; 503 + retry) |
| `POST` | `/api/customizer/designs/{id}/clone` | Bearer | New draft + token; Put-copies source bytes |
| `GET` | `/api/v1/admin/customizer/health` | `jobs.read` | Per-type depths via `BoundQueue` + `CountsByType` |

**Not k8s probes.** Use `/healthz` and `/readyz`. Guest `/api/customizer/*` responses are `Cache-Control: no-store`. Depths stay admin-only (backlog/failure recon). Contract: [`docs/architecture/api.md`](../../docs/architecture/api.md).

Sync jobs: `integration.sync.metaldogs.obs.ping` (F3 sample); `integration.sync.metaldogs.generate` (C4); `integration.sync.metaldogs.prepare_production_artwork` (C6 stub terminals `pending_spec`; approve leaves failed/done alone until G5). Platform injects `request_id` on Enqueue; MD logs also emit `correlation_id` (same value). Platform contract: [shopanda#568](https://github.com/akarso/shopanda/pull/568) at pin `a8cbcb18`, plus Metal Dogs residuals `App.BoundQueue()` and sync-handler `request_id`→ctx reinjection (see [`docs/architecture/overview.md`](../../docs/architecture/overview.md) deviations).

Generation config (C4/C7): `plugins.metaldogs.generation.provider` (fake only), `max_retries`, `max_attempts_per_design`, `cooldown_seconds` (default 30).

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

`testutil.OpenIsolated` — unique schema per call, forced `search_path`. CI: `integration-metaldogs` canaries.

Later job types: `integration.sync.metaldogs.<name>` (ADR-0003).

## Offline eval (G0)

With the plugin enabled (`SHOPANDA_PLUGINS_METALDOGS_ENABLED=true`):

- **With media:** full Init — `AssetStore`, customizer HTTP, obs.ping, and `metaldogs:eval`.
- **Without media (eval-only):** Init still succeeds and registers `metaldogs:eval` only — no customizer HTTP / AssetStore (F4 fail-closed for HTTP).

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

The loader checks all three files before it writes. A bad file exits non-zero and names the file, row and column. Existing variant SKUs are not inserted again. Status of a product that already exists is left as it is, including `active` after a later publish. A price or stock row is written when the stored value differs from the file; an equal value is skipped. A second run with unchanged files writes nothing.

`parent_sku` is written in the same insert when the product is created. On a product that already exists, a missing `parent_sku` is written before any new variants, as its own update (not in the variant transaction). A stored value that differs from the CSV is an error and is left unchanged.

Shopanda assigns the tax class from the product type. `configurable` maps to the standard class (`catalog.TaxClassCode`). This command writes no tax setting. The shop seed's standard rate for PL is 2300 basis points (23%).

### Price format

The CSV holds a human decimal. The command converts it to minor units (PLN grosz) with no rounding, then the core price importer stores that integer.

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

1. Replace the placeholder in `prices.csv` with the real net amount and run `metaldogs:catalog-seed`. Rows that already match the file are skipped; a different amount is updated.
2. Set the launch quantity in `stock.csv` (real stock, or a high reservable quantity) and run the command again. The same skip-if-equal rule applies.
3. Set the Shopanda product `mdogs-tee` to status `active` in the admin catalog. Leave `products.csv` as `draft`. The loader rejects any other status, and it does not change the status of a product that already exists.

### Limits

- Product, price and stock writes use separate transactions. A failure after the product write can leave a product without the matching price or stock. Validation of the files still happens before any write.
- CSV row numbers assume one record per line.
- Leading zeros in a price are accepted (`080,49` → `8049`).
