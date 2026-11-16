# CR-19: E2 cart line design attach

- Status: **open** (implementation complete; merge pending)
- Slices: `E2`
- Milestone progress: M3 → E2

## What shipped

- `POST /api/customizer/designs/{id}/add-to-cart` adds an approved design the caller owns to the storefront session cart (`shopanda_storefront_cart`). The JSON response is `{ "cart_url": "/cart" }`. The cart id is only the cookie
- Cart-line extension `metaldogs.design_id`: public read, plugin-only write, snapshot storage, UUID-shaped. The trusted-write context is created only for `AddItem`
- Same design and variant again does not change quantity. A different design on that variant, or a custom-tee line that already has no design id, is `409` and leaves the cart unchanged. Another size is a second line
- `cart.validate` blocks a custom-tee line whose design is missing, unknown, expired, or no longer approved. Other products are untouched. The hook does not check ownership
- `cmd/api/metaldogs_cart.go` supplies the session-cart port. The plugin does not import cart infrastructure adapters. No new migration and no new flag
- Adding to the cart does not change the design's status, versions, or expiry

## Verification results

| Item | Result |
| --- | --- |
| `SHOPANDA_TEST_DSN=postgres://shopanda:changeme@127.0.0.1:5433/shopanda_task_e2?sslmode=disable go test -count=1 -p 1 -timeout 25m ./plugins/metaldogs/... ./cmd/api/...` | Exit 0 in 26s. Code revision `82f52ba05ae7012e8d28b54c0e108eadab26e2fd`. This commit adds documentation only; the suite does not execute it |
| `make docs-policy` (`bash .github/scripts/check-product-docs-links.sh`) | Exit 0 on the same worktree |
| Known red on the upgrade base, not part of this run | `TestImportBoundary` (`job_fence.go`), `store_credit`, notification dedupe, OpenAI smoke |
| Flaky on the base, not an E2 failure | `TestGenerateWorker_OverlapProcess_WinnerStaysSucceeded` (timeout observed on base; not changed in E2) |

Log: [`evidence/CR-19-e2-cart-design-attach-test.log`](evidence/CR-19-e2-cart-design-attach-test.log).

## Tests added

- `plugins/metaldogs/cart_add_test.go` — approved design, owner token, one line, quantity one, field visible, `cart_url` `/cart`; unapproved `409`; foreign, unknown, expired `404`; variant outside the tee `422`; extra JSON fields `422`
- `plugins/metaldogs/cart_add_rules_test.go` — repeat does not change quantity; different design `409` and cart unchanged; second size; quantity raised on the cart API stays; remove and add again
- `plugins/metaldogs/cart_design_field_test.go` — public cart add and update of `metaldogs.design_id` return `403` `forbidden_plugin_only_field` and store nothing
- `plugins/metaldogs/cart_validation_test.go` — tee line without a design blocks; another product does not; design later unapproved, expired, or removed blocks; plugin enabled without media still blocks a tee line that has no design id
- `plugins/metaldogs/httpapi/addtocart_test.go` — trailing data after the JSON object is `422`
- `plugins/metaldogs/adapters/sessioncart/cart_rules_test.go` — same-design repeat, different design, bare tee line; `GetCart` / `CreateCart` receive a context with no plugin write
- `cmd/api/metaldogs_cart_test.go` — enabled plugin refuses to start when the session-cart port cannot be bound; the skip path logs a warning

## Flags / migrations / rollback

- No new plugin migration and no new flag. The route and the hook exist only when `plugins.metaldogs.enabled` is true
- Rollback: disable the plugin or revert the change. Cart lines already written keep `metaldogs.design_id` until the line is removed. The E1 seed stays draft with stock 0

## Documentation updated

- [x] `docs/ROADMAP.md`: E2 **DONE** (CR-19). Safe delivery says a missing design id blocks a custom tee and is inert for every other product. §1 extension-field row notes the E2 contract tests
- [x] `docs/architecture/api.md`: route, body, `200` `cart_url`, error table (`403` on other paths, `404`, `409` including a bare tee line, `422`)
- [x] `docs/architecture/flows/design-to-order.md` and `checkout-snapshot-and-fulfillment.md`: session cart, no cart id in JSON, trusted write on `AddItem`, snapshot copy at checkout
- [x] `docs/architecture/domain-model.md`: cart-line extension note. No new plugin entity
- [x] No new ADR. Decisions sit on ADR-0005 (snapshot copy), ADR-0006 (status unchanged by add), ADR-0007 (owner token, then the cart session), ADR-0009 (one tee, slug `mdogs-tee`)
- [x] Runbooks: no new job, scheduler task, or flag. Failure modes for this route are the API error table
- [x] `plugins/metaldogs/README.md` lists the route. `docs/cr-rounds/README.md` links this record. `AGENTS.md` commands unchanged
- [x] Safe delivery: plugin master switch. Hook does nothing for non-tee products
- [x] Privacy/cost: the design id is visible to the cart holder. The owner token is not stored on the cart. No new logging of photos, tokens, or signed URLs. Rate limit is O4

## Accepted gaps

- Guest cart merge into a customer cart copies `metaldogs.design_id` without the add path or hooks. E3 re-validates at checkout
- A signed-in customer is written to the guest cookie cart. `/cart` loads the customer cart and ignores that cookie, so the line is not on `/cart` after the redirect. Guest session carts are the supported path
- Rate limiting for the route is O4
- A custom-tee line that already exists on that variant with no design id is `409`. The customer removes the line first
- Concurrent adds of different designs on the same session cart and size can both pass the pre-check. Core `AddItem` upserts the extension before `carts.Save`, and a failed version check does not roll that upsert back (Shopanda roadmap, phase 17). E3 re-validates at checkout
- Each cart read runs `variants.FindByID` per line, lists `extension_values`, and loads the design for each custom-tee line. The cost is accepted for this slice
- `metaldogs.design_mismatch` (`DesignMismatchCode`) is reserved. It is not emitted until a template-to-product mapping exists (X2)
- A process death after the pre-write and before `AddItem` can leave a design id with no line. E3 re-validates the design. No core change in this slice
- The headless cart API still exposes the cart id. This route does not

## Follow-ups

- E3: checkout re-validation, including merged carts and the concurrent-add race
- E4: order snapshot from the copied design id; design → `ordered`
- S5: size picker and the button that calls this route
- O4: rate limit
- X2: emit `metaldogs.design_mismatch`
