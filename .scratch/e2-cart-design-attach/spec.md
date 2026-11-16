Status: ready-for-agent (tickets in issues/ once to-tickets has run)
Slice: E2 · Cart line design attach (docs/ROADMAP.md) — size M
Related: ADR-0005 (order design snapshot), ADR-0006 (design status model), ADR-0007 (guest design ownership tokens), ADR-0009 (catalog modelling), E1 (catalog), E3 (checkout validation, consumer), E4 (order snapshot, consumer), S5 (storefront, consumer)

## Problem Statement

A customer can approve a design in the customizer, but the approved design cannot be bought. There is no way to put the Metal Dogs custom tee in the Shopanda cart together with the design it should be printed with. The generic cart API would let any client attach any value to a cart line, so a line cannot be trusted to carry a real, approved, owned design. Without a trustworthy link between a cart line and an approved design, checkout (E3) and the order snapshot (E4) have nothing to validate or copy.

## Solution

The customer presses "add to cart" on an approved design, chooses a size, and the design lands in their Shopanda cart as one line for that size. The line carries the design id as a field that only the Metal Dogs plugin can write. Adding the same design to the same size again changes nothing. Adding a different design to a size that already has one is refused. The generic cart API refuses to set the field. A custom tee line that has no design, or whose design is no longer valid, blocks checkout. Everything is inert for any other product and for installations where the plugin is disabled.

## User Stories

1. As a customer, I want to add my approved design to the cart in a chosen size, so that I can buy the tee with my design on it.
2. As a customer, I want the cart to remember which design is on which line, so that I can see what I am buying.
3. As a customer, I want to add the same design in two different sizes, so that I can order matching tees.
4. As a customer, I want adding the same design to the same size twice to be harmless, so that a double click or a retry does not change my order.
5. As a customer, I want a clear refusal when I try to put a different design on a size that already has one, so that my earlier choice is not silently replaced.
6. As a customer, I want to remove a design line and add it again, so that I can change my mind.
7. As a customer, I want to change the quantity of a design line in the cart page to two or more, so that I can order several tees with the same print and size.
8. As a customer, I want an unapproved design to be refused with a message that it is not ready, so that I know to finish approving it.
9. As a customer, I want a design that is not mine, does not exist or has expired to look "not found", so that nobody can probe other people's designs.
10. As a guest customer, I want to add my design without signing in, so that buying does not require an account.
11. As a guest customer, I want my cart to be the one in my browser session, so that the design line appears on the Shopanda cart page I am sent to.
12. As a customer, I want to be taken to the Shopanda cart page after adding, so that I can continue to checkout.
13. As a customer, I want a refusal when I pick a size or variant that is not part of the custom tee, so that I cannot attach my design to the wrong product.
14. As the shop operator, I want only the Metal Dogs route to be able to set the design on a cart line, so that a line's design can be trusted.
15. As the shop operator, I want the generic cart API to refuse to set the design field, so that a client cannot attach an arbitrary design to any line.
16. As the shop operator, I want a custom tee line without a design to block checkout, so that I never receive an order that cannot be printed.
17. As the shop operator, I want a design line whose design stopped being approved, expired or was removed to block checkout, so that stale designs never become orders.
18. As the shop operator, I want other products in the cart to be unaffected, so that selling anything else keeps working.
19. As the shop operator, I want the feature to follow the plugin's existing master switch, so that I do not manage another flag.
20. As the shop operator, I want a design's retention clock left alone by adding it to the cart, so that adding to cart has no side effects on the design.
21. As a developer working on E3, I want every design line to carry the design id on the cart line, so that checkout can re-validate it.
22. As a developer working on E4, I want the design id to be copied to the order line by the core at checkout, so that the snapshot step can find the approved design version.
23. As a developer working on S5, I want one predictable route that adds a design in a size and tells me where to send the customer, so that the storefront does not need to know cart internals.
24. As a developer, I want the plugin to reach the cart through a narrow port supplied by the composition root, so that the Shopanda core stays untouched.
25. As a developer, I want the plugin never to take the field's trusted-write marker or codes from request input, so that trust cannot be forged.
26. As a developer, I want tests to run the route on an isolated database with core and plugin migrations, so that behaviour is observed the way a customer would see it.
27. As a reviewer, I want the roadmap wording corrected so that "inert without design id" applies only to non-tee products, so that the docs match the decision.
28. As a reviewer, I want the known gaps (cart merge, signed-in customer, rate limit) written down, so that nobody assumes they are covered.
29. As a future maintainer, I want the route, the error mapping and the cart flow documented, so that the customizer API reference stays complete.
30. As a future maintainer, I want the evidence of this slice recorded in a CR round file, so that the slice has a closed record like E1.

## Implementation Decisions

- **Route.** A new Metal Dogs public route adds an approved design to the caller's cart: path `POST /api/customizer/designs/{id}/add-to-cart`. It is covered by the existing customizer prefix that skips customer JWT parsing. Authorisation is the design owner token (ADR-0007) in the Authorization header; tokens in the query string are rejected as in the other design routes. The request names the variant to add. The success response tells the storefront where the cart page is so S5 can redirect.
- **Cart identity.** The route uses the storefront session cart (the existing cart cookie), creating a cart when none exists. It does not accept or return a raw cart id. After the add, the cart session owns the line and the Shopanda checkout rules apply; the design token is not needed afterwards (ADR-0007).
- **Field.** A cart-line extension field `metaldogs.design_id`, registered by the plugin: public (so cart responses show it), plugin-only write, snapshot storage mode, with a UUID-shaped validation. Plugin-only write means the public cart API, the SSR cart pages and the admin values API refuse to set it (403, nothing stored). The route writes it through a trusted write context that names the field and the plugin; the field code and marker are fixed in code and never taken from request input. Snapshot mode means the core copies the value to the order line at checkout.
- **Cart access.** The plugin does not hold a cart service. The composition root supplies a narrow "add a line with extension values" port to the plugin, in the same way E1 supplied catalog repositories. The Shopanda core is not modified and the new composition file is added to the upstream-deviations allowlist.
- **Add semantics.** The line identity in the cart is the variant. Rules, applied by the route before the cart is touched:
  - same design, same variant already in cart → success, no change in quantity, existing cart returned;
  - a different design already on that variant → 409 conflict, nothing changed;
  - the variant is already a custom-tee line with no design id → 409 conflict, nothing changed; the customer removes that line before a design can be attached;
  - the same design on a different variant → allowed, a second line;
  - a new line is added with quantity one; the customer may raise the quantity on the Shopanda cart page (any quantity of one or more), and the route never raises it.
- **Eligibility.** The design must belong to the caller (token), exist, be unexpired and be `approved` (not `ordered`, not `expired`). The chosen variant must belong to the custom tee product (the product identified by its configured slug; one product at MVP, a template-to-product mapping is deferred to X2).
- **Error mapping.** Foreign, unknown or expired design → 404. Design not `approved` → 409. Variant not part of the custom tee, or invalid request → 422. Plugin-only write attempted through any other path → 403 (core behaviour). Errors use the existing mapped application error kinds.
- **Cart validation.** The plugin registers a cart validation hook. For a cart line whose variant belongs to the custom tee: a missing design id is a blocking issue ("design required"); a present design id that no longer points to an approved, unexpired design is a blocking issue (unknown or removed: "design not found"); a design that does not match the custom tee is a blocking issue once a template-to-product mapping exists (X2; the code is reserved and not emitted yet). The hook does not check ownership (no token available); ownership was checked on add and the field cannot be written by anyone else. For every other product the hook does nothing. A stale custom-tee line blocks cart read and a mutation of that line (add or quantity change). It does not block adding, removing, or changing another line: core `RemoveItem` deletes that line's extension values before `cart.validate` and does not restore them when validation fails, so a blocking issue on an untouched line would leave the removed line in the cart without its design id. This replaces the roadmap wording "inert without design id" for the custom tee only; the roadmap line is corrected in this slice.
- **Design is not modified.** Adding to the cart does not change the design's status, versions or expiry (ADR-0005, ADR-0006); `ordered` belongs to E4.
- **Switch.** No new flag. The route and hook exist only when the plugin is enabled, which is the roadmap's "flagged" delivery.
- **Cart lifecycle facts to respect.** Removing a line clears the field; the route therefore writes it on every add. Merging a guest cart into a customer cart copies the field without running the add path; this is a known gap (see Out of Scope). The core copies snapshot-mode values to the order line.
- **Docs.** The customizer API reference gains the new route and its error table; the design-to-order flow is aligned with the decisions here (session cart, no cart id exposed); the roadmap E2 card is corrected; a CR round file records tests, evidence and decisions.
- **Out-of-process changes.** None to the Shopanda core or migrations. The pin already includes plugin-only extension fields (pin f58a4157).

## Testing Decisions

- **A good test here** observes only external behaviour: call the route as a customer would, then read the cart through the public cart API and check lines and their `extensions`. It does not inspect tables, the extension store or internal services, and it does not assert how the plugin is wired.
- **One seam.** Input: HTTP requests to the plugin route and to the public cart API on a server built over an isolated database that has the core and plugin migrations applied, with the catalog seeded and the product published with stock set by the test (the seed itself stays draft with stock zero). Output: the public cart API responses (line items, quantities, extensions) and, for checkout blocking, the cart validation outcome exposed by the cart API.
- **Cases through that seam:**
  - approved design, valid variant → one line, quantity one, field present, redirect target returned;
  - repeat of the same request → identical cart, quantity unchanged;
  - same design on another size → two lines;
  - different design on the same size → 409, cart unchanged;
  - quantity raised through the cart API → accepted; repeat add afterwards still does not change it;
  - unapproved design → 409; foreign token, unknown id, expired design → 404; variant outside the custom tee → 422;
  - generic cart API tries to set the field (add and update paths) → 403 and nothing stored;
  - custom tee line added through the generic API without the field → cart validation reports a blocking issue; a non-tee product is unaffected;
  - design that stops being approved after being added → cart validation reports a blocking issue;
  - removing the line and adding again works;
  - plugin disabled → no route, no hook (existing wiring test style).
- **New test infrastructure.** Only what the seam needs: a helper that builds the real storefront and cart over the isolated database and a helper that sets stock for the test variants. The catalog helpers from E1 are reused.
- **Prior art.** The catalog seed tests (real storefront over an isolated database, product publishing helper), the design route tests with in-memory fakes and the auth middleware wrap, the plugin wiring tests, and the core cart service tests with hooks and extensions. Tests skip when the test database DSN is unset; plugin packages run serially in CI.

## Out of Scope

- Checkout re-validation and guest checkout end to end (E3); order snapshot (E4); admin artwork access (E5); confirmation thumbnail (E6).
- Storefront size picker and the add-to-cart button itself (S5).
- Rate limiting and abuse control for the new route (O4).
- A template-to-product mapping in the data model; a second garment (X2).
- Unlimited stock or the M3 stock policy; publishing the product.
- Preserving the field when a guest cart is merged into a customer cart through hooks: the core copies the field without hooks, and E3 re-validates at checkout. Documented as a known gap.
- A signed-in customer. The route skips customer JWT parsing and always uses the guest session cookie. The storefront cart page loads a signed-in customer's cart by customer id and ignores that cookie, so the added line is not on `/cart` after the redirect. Guest session carts are the supported path.
- Any change to the Shopanda core or to existing migrations.
- Closing the race of two concurrent adds of different designs on the same session cart and size. Both can pass the pre-check. Core `AddItem` upserts the extension before `carts.Save`, and a failed optimistic version check does not roll that upsert back, so the line can keep the design whose save lost. E3 re-validates at checkout. No core change in this slice.

## Further Notes

- The line identity is the variant, so one cart line holds one design; two designs in the same size cannot coexist, which is why a different design on the same size is refused rather than added.
- The core refuses a purchase of a draft product or one with no stock. Tests must publish the custom tee and give it stock; do not change the E1 seed to make tests pass.
- Because the field is public, cart responses show the design id to the cart holder. This is intended: the id is not a secret and the owner token is never stored on the cart.
- Trust marker rule from upstream: the trusted-write context is per field code and is stripped at event, pricing, checkout and job dispatch points. The adapter creates that context only for `AddItem`. `GetCart` and `CreateCart` receive a stripped context.
- Known accepted limits at this slice: the cart merge gap above, the signed-in customer gap above, the concurrent-add race above, and the shared cart-id exposure of the headless cart API (the route avoids it by using the session cart).
