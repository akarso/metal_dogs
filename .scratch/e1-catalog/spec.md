Status: ready-for-agent (4 tickets in issues/)
Slice: E1 · Catalog product and variants (docs/ROADMAP.md) — size S, expanded by the test helper below
Related: ADR-0009 (catalog modelling), ADR-0005, E2 (consumer of this catalog)

## Problem Statement

The Metal Dogs customizer attaches an approved design to a cart line, but there is nothing to put in the cart. The shop has no "Metal Dogs custom tee" product, no size variants, no SKUs and no price. Catalog setup done by hand in the admin is not repeatable, cannot be reviewed in a PR and cannot be recreated on a fresh environment. The operator also wants the product to stay invisible and unbuyable until M3, and wants prices stored net so a future VAT change does not require touching the catalog.

## Solution

Ship the catalog as data in the repository and a manual command that loads it. One Shopanda product per garment type ("Metal Dogs custom tee") with five size variants (S, M, L, XL, XXL), each with its own SKU, a net price in PLN and zero stock. Running the command on an empty database creates the product as a draft. Running it again changes nothing. The product is hidden from the storefront until someone deliberately publishes it at M3. Prices are written by a human as ordinary decimals ("80,49" or "80.49") and the command converts them safely.

## User Stories

1. As the shop operator, I want the custom tee defined in files checked into the repository, so that the catalog is reviewable and reproducible.
2. As the shop operator, I want to load the catalog with one manual command, so that I control when catalog data changes.
3. As the shop operator, I want to run that command twice and get no duplicates, so that re-runs and retries are safe.
4. As the shop operator, I want the product created as a draft, so that nothing about it is visible to customers before M3.
5. As the shop operator, I want the import to never publish the product, so that publication is always a separate, deliberate decision.
6. As the shop operator, I want the product to have five size variants S, M, L, XL and XXL, so that customers can pick a size later.
7. As the shop operator, I want predictable SKUs of the form MDOGS-TEE-S … MDOGS-TEE-XXL, so that orders, fulfilment and support can refer to them unambiguously.
8. As the shop operator, I want each variant to carry its size as an attribute, so that the size picker (S5) can read it from the catalog.
9. As the shop operator, I want one price for all sizes at MVP, so that pricing is simple until demand is validated.
10. As the shop operator, I want prices stored net, so that a VAT rate change does not require a catalog change.
11. As the shop operator, I want the placeholder price clearly marked in the data file, so that I remember to set the real price before M3.
12. As the shop operator, I want to write prices as "80,49" or "80.49", so that I do not have to convert to minor units by hand.
13. As the shop operator, I want a price like "1.234,56" or "1,234.56" understood correctly, so that thousands separators do not break the import.
14. As the shop operator, I want a price with more than two decimal places rejected, so that nothing is silently rounded.
15. As the shop operator, I want a price containing spaces, currency symbols or text rejected, so that a typo cannot become a wrong price.
16. As the shop operator, I want an empty or missing price rejected, so that a variant never ends up free by accident.
17. As the shop operator, I want a rejected CSV to stop the whole import with a clear message naming the row and column, so that I can fix it quickly.
18. As the shop operator, I want a failed import to leave the database unchanged, so that I never have a half-created product.
19. As the shop operator, I want each variant to start with stock zero, so that the product is not purchasable before M3 even if it were published by mistake.
20. As the shop operator, I want the standard 23% VAT class to apply automatically, so that I do not configure tax per product.
21. As a developer working on E2, I want the product and variants to exist after one command on a fresh database, so that I can attach designs to real cart lines.
22. As a developer, I want the variants readable through the public storefront API, so that I do not depend on internal tables.
23. As a developer, I want the catalog command to leave the Shopanda core untouched, so that the upstream pin stays clean.
24. As a customer, I want an unpublished product to be absent from the storefront API, so that I never see something I cannot buy.
25. As a customer, once the product is published later, I want to see all five sizes, so that I can choose mine.
26. As a reviewer, I want ADR-0009 catalog modelling marked Accepted in the same change, so that the decision record matches reality.
27. As a reviewer, I want the net-to-gross conversion documented, so that nobody stores gross prices by mistake.
28. As a future maintainer, I want the publish-at-M3 steps written down (set status active, load real stock), so that launch does not depend on memory.
29. As a future maintainer, I want the import to run against a fresh test database in CI-style isolation, so that regressions in the catalog surface early.

## Implementation Decisions

- **Catalog data lives in the repository** as human-edited CSV files owned by the Metal Dogs plugin: one for the product and its variants, one for prices, one for stock. They are the single source of truth for the custom tee.
- **One product, five variants.** Product "Metal Dogs custom tee", parent SKU MDOGS-TEE, product type configurable (a type label in this Shopanda version, not a parent–child mechanism). Variants S, M, L, XL, XXL with SKUs MDOGS-TEE-{size}. Size is stored as the variant attribute `size`. Templates and designs are not products or variants (ADR-0009).
- **Status.** The product is created as `draft` and the command never changes status of an existing product. Status in the core product importer is create-only, which matches this rule. Publication at M3 is a separate action: set status to `active` and load real stock.
- **Tax.** No tax setting is written. Tax class derives from product type in the core, so the configurable product gets the standard class, 23% for PL.
- **Price.** One net price per variant, currency PLN, global (no store-specific rows). Placeholder: 80,49 PLN net, which is 99,00 PLN gross at 23%. The file marks it as a placeholder. Gross is always computed from the current rate and is never stored. Because the core price importer expects minor units and the Shopanda core is a pinned upstream that is not modified, the plugin converts human-readable decimals to minor units itself.
- **Stock.** Quantity 0 for every variant. The core has no unlimited-stock option, only quantities, so zero is deliberate for the pre-M3 period. Revisit at M3 (real quantity or a high reservable quantity, per ADR-0009).
- **Command.** A new manual plugin CLI command `metaldogs:catalog-seed`, registered alongside the existing `metaldogs:*` commands. It needs the database. It reads the three files, validates all of them first, then writes through the core import services (products, then prices, then stock). It is never run by migrations or at startup.
- **Idempotency.** Keyed by SKU/slug as the core importers already are. A second run produces no new rows and no changes.
- **Atomicity.** Validation happens before any write. If validation fails, nothing is written. The command exits non-zero with a message naming file, row and column.
- **Price parsing rule.** Accept digits with at most one decimal part of at most two digits. A single separator (comma or dot) is the decimal separator. If both appear, the last one is decimal and the other is a thousands separator ("1.234,56", "1,234.56"). More than two fractional digits, spaces, currency symbols, letters, negatives, zero, and empty cells are errors. No silent rounding.
- **Visibility.** The four visibility axes keep their defaults. Hidden-ness before M3 comes from `draft` status, which the storefront read path already enforces. The visibility-enforcement configuration flag defaults to off in this fork and the spec does not depend on it.
- **Documentation.** Plugin README gains a short catalog section (files, command, price format, net→gross formula, publish-at-M3 steps). `docs/architecture/api.md` is unchanged because no new HTTP endpoint is added. ADR-0009: the catalog modelling part becomes Accepted with the E1 close note; the fulfilment part stays Proposed until P0. The roadmap marks E1 done with the CR reference. A CR round file is added following the existing CR-NN convention.
- **Out-of-process changes.** None to the Shopanda core, to migrations, or to any existing module.

## Testing Decisions

- **A good test here** observes only external behavior: run the command, then read the catalog through the public storefront API. It does not inspect tables, repositories or internal services, and it does not assert how the command is wired.
- **One seam.** Input: the `metaldogs:catalog-seed` command run against an isolated database that has the core and plugin migrations applied, using the repository's CSV files (and purpose-built bad CSV files for error cases). Output: the public storefront HTTP API (`GET /api/v1/products/{id}/variants` and the product read endpoints).
- **Cases through that seam:**
  - fresh database → five variants with the expected SKUs, `size` attribute, net price and zero stock;
  - second run → identical result, no duplicates;
  - product is `draft` → absent from the storefront API;
  - after setting status to `active` → present, with all five sizes;
  - price formats "80,49", "80.49", "1.234,56" and "1,234.56" → correct stored net amount;
  - price formats "80,499", "80 zł", "80 PLN", "-5", "0", "" and "abc" → command fails, message names row and column, database unchanged;
  - mixed file with one bad price among good rows → nothing written.
- **New test infrastructure.** One helper in the plugin test utilities that applies the core migrations to the isolated test schema (existing helper applies only plugin migrations). This is the only new seam-support code; there are no separate unit tests for the parser.
- **Prior art.** The existing isolated-database helper and its `SHOPANDA_TEST_DSN` convention (tests skip when the DSN is unset); CI runs plugin packages serially. Existing plugin tests for uploads, designs and generation show the style.
- **Net-to-gross check.** One assertion that gross computed from the stored net price and the standard rate equals 99,00 for the placeholder, to catch net/gross mix-ups.

## Out of Scope

- Showing gross, net and VAT to customers (storefront, S5).
- Attaching a design to a cart line (E2), checkout validation (E3), order snapshot (E4).
- Publishing the product, setting real stock, setting the real price (all M3 launch steps).
- Hoodies or any second garment type (X2).
- Template-dependent pricing.
- Reduced or zero tax classes, multiple currencies, store-specific prices.
- Admin UI for catalog editing.
- Any change to the Shopanda core importers, domain or migrations.
- The POD provider and print specification (P0) — ADR-0009 fulfilment part stays Proposed.

## Further Notes

- The "configurable" product type is only a label in this Shopanda version; there are no option axes. A configurable product here is one product with several variants. Do not build a parent–child layer.
- Zero stock plus draft status means two independent protections; M3 launch must remove both.
- The visibility-enforcement flag is off by default in this fork. Hidden-ness relies on draft status; if the flag is ever turned on, the axes additionally require stock, price and active status.
- Whether stored prices are net or gross in the core's price pipeline was not confirmed from code during grilling. The developer must confirm in the tax calculator before writing the price file and document the finding; if the core treats stored amounts as gross, stop and raise it, because the decision "store net" would need revisiting.
- Branching: E1 work starts from the branch that carries the code-comment rules in AGENTS.md; comments in new code follow them.
