# CR-18: E1 catalog product and variants

- Status: **open** (implementation complete; merge pending)
- Slices: `E1`
- Milestone progress: M3 → E1

## What shipped

- Catalog data in `plugins/metaldogs/catalog/`: `products.csv`, `prices.csv`, `stock.csv`
- Manual command `metaldogs:catalog-seed` (plugin enabled, database required; not run by migrations or startup)
- One draft configurable product "Metal Dogs custom tee" (slug `mdogs-tee`, parent SKU `MDOGS-TEE`), variants S–XXL (`MDOGS-TEE-S` … `MDOGS-TEE-XXL`), attribute `size`
- Net PLN prices. Placeholder `80,49` is stored as `8049` minor units. Standard tax class comes from product type `configurable`; the command writes no tax setting
- Stock quantity `0`
- Re-run skips existing SKUs and does not change an existing product's status. A price or stock row is written only when the stored value differs
- `parent_sku` is stored on create. A stored value that differs from the CSV is an error and is left unchanged
- `cmd/api/metaldogs_catalog.go` builds the core repos so the plugin does not import infrastructure adapters
- ADR-0009 catalog modelling Accepted. The fulfilment table stays Proposed until P0

## Verification results

| Item | Result |
| --- | --- |
| `go test ./plugins/metaldogs/... -count=1` | Exit 0 at `bc13e24e82e57e67aa37b323c39048272e4e8c7d`. DSN `postgres://shopanda:changeme@127.0.0.1:5433/shopanda_task_e1?sslmode=disable` |
| Docs policy | `check-product-docs-links.sh` exit 0; `check-upstream-deviations.sh` exit 0 (pin `43f0a1ed`, 24 allowlisted paths). These two commands are the `make docs-policy` target |
| Known red on the upgrade base, not part of this run | `TestImportBoundary` (`job_fence.go`), `store_credit`, notification dedupe, OpenAI smoke |

Log: [`evidence/CR-18-e1-catalog-test.log`](evidence/CR-18-e1-catalog-test.log).

## Tests added

- `plugins/metaldogs/catalog_seed_test.go` — fresh database → five SKUs, `size`, net `8049`, `in_stock` false, placeholder gross `9900`; draft absent from the storefront; after status `active`, all five sizes; second run identical; `80.49`, `1.234,56` and `1,234.56`; bad prices (`80,499`, `80 zł`, `80 PLN`, `-5`, `0`, empty, `abc`) name `prices.csv` row and column and write nothing; one bad row among good rows writes nothing; `parent_sku` mismatch leaves the stored value; product repo without `WithTx` writes nothing
- `plugins/metaldogs/testutil` — `OpenIsolatedWithCore` applies core and plugin migrations on the isolated schema

## Flags / migrations / rollback

- No new plugin migration and no new flag. The command is registered only when the plugin is enabled
- Rollback: revert the change. A database already seeded keeps the draft product until someone removes it in Shopanda. Re-running the command does not delete or unpublish it

## Documentation updated

- [x] `docs/ROADMAP.md`: E1 **DONE** (CR-18); §1 public-offer row notes the E1 guest reads
- [x] Architecture: no new HTTP boundary and no new plugin domain type. `docs/architecture/api.md` unchanged. `docs/architecture/overview.md` lists `cmd/api/metaldogs_catalog.go` on the fork allowlist
- [x] ADR-0009 catalog modelling Accepted with an E1 close note; fulfilment stays Proposed until P0. Index updated
- [x] Runbooks: no new job or failure mode. Publish-at-M3 steps are in `plugins/metaldogs/README.md`
- [x] `plugins/metaldogs/README.md` catalog section; `docs/cr-rounds/README.md` links this record
- [x] Safe delivery: product stays `draft` with stock 0. Draft status and zero stock are two independent protections
- [x] Privacy/cost: catalog rows are product data, not customer photos. No new logging of personal data

## Accepted gaps

Documented in the plugin README. Not fixed in E1:

- Product, price and stock writes use separate transactions
- CSV row numbers assume one record per line
- Leading zeros in prices are accepted (`080,49` → `8049`)

## Follow-ups

- M3 launch: real net price, real stock, product status `active`
- P0: fill the ADR-0009 fulfilment table
- E2: attach an approved design to a cart line for this product
