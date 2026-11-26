# ADR-0009: POD provider, production file specification, and catalog modelling

- **Status:** Catalog modelling **Accepted** (E1, CR-18, 2026-11-12). Fulfilment provider and production file specification remain **Proposed** until `P0`.
- **Date:** 2026-10-15 (catalog modelling accepted 2026-11-12)
- **Related slices:** `P0`, `P1`, `P2`, `G5`, `G5a`, `E1`, `E2`, `X2`
- **Related ADRs:** [ADR-0005](0005-immutable-order-design-snapshot.md)

## Context

Fulfilment is print-on-demand. The production file must meet the provider's technical requirements (resolution, dimensions, transparency, colour profile), which cannot be assumed from the preview. A physical sample must be verified before sales start. Shopanda (verified) models products, variants, SKUs, pricing, tax, and inventory reservations; one product line = one SKU plus extension values.

## Decision (catalog modelling, decided now)

1. **One Shopanda product per garment type** ("Metal Dogs custom tee"; later "custom hoodie"), with **size variants** (S–XXL) as SKUs. Templates and designs are **not** products or variants; the design is attached to the cart line via the extension field `metaldogs.design_id`.
2. Price is per garment type and size, not per template, at MVP. Template-dependent pricing, if ever needed, is a pricing-pipeline step, not a variant explosion.
3. Inventory: POD garments are treated as unlimited stock or a high reservable quantity, configured in Shopanda; the plugin does not manage stock.
4. Catalog setup is delivered as a repeatable seed/CSV import checked into the repository, not manual admin edits.

### E1 close (CR-18)

Catalog modelling above is Accepted. `plugins/metaldogs/catalog/` plus `metaldogs:catalog-seed` create one configurable product, "Metal Dogs custom tee" (slug `mdogs-tee`, parent SKU `MDOGS-TEE`), with variants S, M, L, XL and XXL (SKUs `MDOGS-TEE-S` … `MDOGS-TEE-XXL`, attribute `size`). Prices are net PLN. The product is created as `draft` with stock quantity 0. Point 3 (unlimited stock or a high reservable quantity) is the M3 choice; this seed does not load it. The command does not set status to `active`. The fulfilment table below stays Proposed until `P0`.

## Decision (fulfilment, to be filled at P0)

| Item | Value |
| --- | --- |
| Provider | _pending_ |
| Print technology (DTG / DTF / screen) | _pending_ |
| File format and colour mode | _pending_ |
| Resolution (DPI) and pixel dimensions per size/print area | _pending_ |
| Transparency requirement | _pending_ |
| Colour profile | _pending_ |
| Max file size | _pending_ |
| API: create order, cancel window, status webhooks, sandbox | _pending_ |
| Idempotency support on order creation | _pending_ |
| Shipping regions and SLA | _pending_ |
| Data-processing terms (customer address, artwork retention) | _pending_ |
| Physical sample result (garment, print quality vs preview) | _pending_ |

Requirements, the candidate comparison, the sample plan, and the empty mapping onto `plugins.metaldogs.production_spec` are in [production-spec.md](../architecture/production-spec.md) ([CR-43](../cr-rounds/CR-43-p0a-production-spec.md)). No provider is chosen. This table stays `_pending_` until that sample is reviewed.

### G5a (CR-36) — config until P0

The table above stays Proposed. The working document for the sample is [production-spec.md](../architecture/production-spec.md) ([CR-43](../cr-rounds/CR-43-p0a-production-spec.md)). Until a reviewed sample fills the table, `prepare_production_artwork` reads `plugins.metaldogs.production_spec` (`width_px`, `height_px`, `dpi`, `require_transparency`, `color_profile` only `srgb`, `upscale_policy` `reject|upscale`). No defaults. The section absent leaves the job on `pending_spec` and the version `pending`. A present section with any field missing or invalid ends that plugin's Init: worker, scheduler, and the jobs CLI refuse to start; `serve` starts with metaldogs inactive (no routes and no jobs). Those keys are not the accepted printer specification and are not hard-coded sizes. The repository holds no printer numbers until P0 records them. One spec per product; one production file per `DesignVersion`. Per-size files stay out of scope.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Product per template (30–300 products) | Catalog explosion; template versioning becomes product versioning; contradicts "no code or catalog change per template" |
| Variant per template × size | Same problem, worse |
| Manual fulfilment forever | Does not scale, but it **is** the M3 path (`P1`) and the permanent fallback |
| Multiple POD providers | Not before quality and demand are validated |

## Consequences

- Positive: catalog stays tiny; adding templates never touches Shopanda catalog; adding a garment type is one product plus a mockup and print spec.
- Negative: the cart line's meaning depends on the extension field. Mitigation: cart hook and checkout step reject lines whose design is missing or not approved.

## Compliance

- Feature test: cart line for the custom tee without a valid `metaldogs.design_id` is rejected.
- `G5a` tests validate production artwork against an explicit `production.Spec` from configuration ([CR-36](../cr-rounds/CR-36-g5a-production-artwork.md)). The accepted P0 `PrintSpec` is not in the tree yet.
