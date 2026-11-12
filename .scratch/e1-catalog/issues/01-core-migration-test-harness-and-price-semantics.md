# 01: Core-migration test harness and price semantics check

**What to build:** A test harness for the Metal Dogs plugin that gives a test an isolated database with both the Shopanda core migrations and the plugin migrations applied, and proves that the public storefront API can be called against it. As part of the same ticket, the developer confirms whether the core treats a stored price amount as net or gross and records the finding in this ticket.

**Blocked by:** None (can start immediately).

**Status:** done

- [x] A test can obtain an isolated database with core and plugin migrations applied; it is skipped when no test DSN is configured, like the existing helper
- [x] A smoke test starts the storefront API against that database and reads an empty product list successfully
- [x] The isolated schema is dropped on cleanup; running the package twice leaves no leftovers
- [x] Finding recorded under a `## Findings` heading in this file: stored price amounts are net or gross, with the code that proves it
- [x] If the core treats stored amounts as gross, work stops and the finding is reported before any other ticket starts (the decision "store net" must be revisited)
- [x] Existing plugin tests still pass

## Findings

Stored price amounts are **net**. The "store net" decision stands; ticket 02 can write the price file.

The price row is loaded unchanged as the line's unit price (`lookupStoredUnitPrice` in `internal/application/pricing/unit_price.go`, applied by `BasePriceStep`). Tax runs after that, in `RateTableCalculator`, and the mode comes from `PricingContext.Meta["tax_mode"]`.

`tax.ModeExclusive` means tax is added on top of the price (`internal/domain/tax/tax_rate.go`). `tax.Calculate` then does `tax = price * rate / 10000` and leaves the item total equal to the stored amount (`internal/domain/tax/calculate.go`; `TestTaxStep_Exclusive` and `TestTaxStep_FullPipeline_Exclusive` in `internal/application/pricing/tax_step_test.go`). `ModeInclusive` is the gross path: tax is extracted from the stored amount and the item total is reduced to net.

Cart and checkout both set that mode to exclusive when the caller does not (`internal/application/cart/service.go` sets `tax_mode` to `tax.ModeExclusive` once a tax country is present; `internal/application/checkout/service.go` sets it the same way). Nothing in that wiring selects inclusive by default. A stored 8049 minor units is 80.49 net, and 23% VAT is added on top.
