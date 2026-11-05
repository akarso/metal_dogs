# ADR-0005: Approval freezes a `DesignVersion`; orders carry an `OrderDesignSnapshot`

- **Status:** Proposed (to be Accepted at close of `C6` and `E4`)
- **Date:** 2026-10-15
- **Related slices:** `C6`, `G5`, `E2`, `E4`, `E5`, `O2`
- **Related ADRs:** [ADR-0006](0006-design-and-job-status-model.md), [ADR-0009](0009-pod-provider-and-catalog-modelling.md)

## Context

Project principle 4: every order is bound to an immutable version of the approved design; later template changes must not alter an existing order. `AGENTS.md`: persist the exact approved artwork, configuration, and checksum with the order snapshot; keep preview separate from production artwork. Shopanda (verified) copies extension-field values from cart to order at checkout and creates immutable orders.

## Decision

1. **Approval creates a `DesignVersion`** (append-only): approved artwork asset id + SHA-256, template id + version, texts, provider, model, prompt version, generation parameters, approved-at. A design has at most one approved version; approving again is idempotent and returns it.
2. After approval the design is **immutable**: no text edits, no photo replacement, no regeneration. "Change it" clones the design into a new draft with a new owner token.
3. Production artwork is prepared **asynchronously from the approved version** (`metaldogs.prepare_production_artwork`) and attached to the `DesignVersion` with its own checksum. Preview assets are never used for production.
4. The cart line carries only `metaldogs.design_id`. **At order creation**, the plugin creates an `OrderDesignSnapshot` per order line that **copies** the version's identifying data (not just a foreign key): artwork asset id and checksum, production asset id and checksum (nullable until ready), template slug and version, texts, provider/model/prompt version, variant SKU. Unique on `order_line_id`.
5. Assets referenced by a snapshot are excluded from retention until the fulfilment record is `shipped` plus the grace period; approved and production artwork retention after that is a policy decision in `O2`.
6. Templates are versioned; publishing a changed template increments the version. In-flight designs keep their pinned version; nothing is migrated.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Reference the design row from the order and read it live | Design can be deleted or expire; template could change; violates principle 4 |
| Store artwork bytes inside the order | Shopanda order is core schema; plugin must not mutate it; bytes belong in object storage |
| Snapshot at cart time | Cart is mutable and may be abandoned; order creation is the commitment point |
| Prepare production artwork at order time only | Delays fulfilment and surfaces spec failures after payment; preparing at approval gives time to fix |

## Consequences

- Positive: orders are fully self-describing; template catalogue evolves freely; operator can reproduce or diagnose any ordered artwork.
- Negative: data duplication between version and snapshot. Accepted deliberately for durability.
- Negative: an approved design cannot be tweaked. Mitigation: clone flow; clear UI copy before approval.

## Compliance

- Unit tests: `DesignVersion` and `OrderDesignSnapshot` have no setters; approve-from-non-`preview_ready` rejected; second approve returns the same version id.
- Feature test: publish a new template version after an order; snapshot and signed artwork unchanged (checksums equal).
- Feature test: duplicate `order.created` delivery produces one snapshot.
