# ADR-0009: POD provider, production file specification, and catalog modelling

- **Status:** Proposed — provider and spec to be decided at close of `P0`; catalog modelling to be Accepted at close of `E1`
- **Date:** 2026-10-15
- **Related slices:** `P0`, `P1`, `P2`, `G5`, `E1`, `E2`, `X2`
- **Related ADRs:** [ADR-0005](0005-immutable-order-design-snapshot.md)

## Context

Fulfilment is print-on-demand. The production file must meet the provider's technical requirements (resolution, dimensions, transparency, colour profile), which cannot be assumed from the preview. A physical sample must be verified before sales start. Shopanda (verified) models products, variants, SKUs, pricing, tax, and inventory reservations; one product line = one SKU plus extension values.

## Decision (catalog modelling, decided now)

1. **One Shopanda product per garment type** ("Metal Dogs custom tee"; later "custom hoodie"), with **size variants** (S–XXL) as SKUs. Templates and designs are **not** products or variants; the design is attached to the cart line via the extension field `metaldogs.design_id`.
2. Price is per garment type and size, not per template, at MVP. Template-dependent pricing, if ever needed, is a pricing-pipeline step, not a variant explosion.
3. Inventory: POD garments are treated as unlimited stock or a high reservable quantity, configured in Shopanda; the plugin does not manage stock.
4. Catalog setup is delivered as a repeatable seed/CSV import checked into the repository, not manual admin edits.

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

The accepted specification is also written to `docs/architecture/production-spec.md` and encoded as `PrintSpec` in the `generation` module, which `G5` validates against.

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
- `G5` unit tests validate production artwork against `PrintSpec` from this ADR.
