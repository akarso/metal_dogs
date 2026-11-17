# CR-20: MIG1 cart path on the public extension SDK

- Status: **closed**
- Closed: 2026-11-16
- Slices: MIG1 (E2 behaviour unchanged)
- Milestone progress: M3 cart SDK

## What shipped

- `plugins/metaldogs` stays a `plugin.Plugin` (`Migrator`, CLI, jobs, media, catalog seed). `Init` stores `pluginsdk.AsHostFor` and calls `Require(CapCart)` and `Require(CapCatalog)`. `Needs()` records the same capabilities so non-serve boots use `AssertNeedsMetExcept(CapCart)`. A full `pluginsdk.Adapt` conversion would replace `Init(*plugin.App)` and was not required.
- Add-to-cart uses `Host.Cart().EnsureStorefrontCart` and `Host.Catalog().FindVariants`. `cmd/api/metaldogs_cart.go`, `cmd/api/metaldogs_cart_test.go`, and `plugins/metaldogs/adapters/sessioncart` are removed. The deviations allowlist no longer names the cart files.
- `WithPluginWrite` wraps only `AddItem`. `EnsureStorefrontCart` and `FindVariants` use the request context. Proven by `TestPlace_WriteGrantOnlyOnAddItem`.
- Pre-write of `metaldogs.design_id` and the failed-add cleanup are removed. On pin `a411c970`, `cart.validate` runs inside `AddItem` before the extension upsert and `CartView` lists stored rows only (`internal/application/cart/service.go`). `UpsertLineExtension` also requires the line to exist, so the public cart port cannot pre-write. `cart.add_item.before` stores the design id for that AddItem goroutine; `cart.validate` uses it only when the line has no stored design id. `Place` releases the slot only when the token still belongs to that call (`TestPlace_InterleavedSameVariantKeepsOwnDesign`). A shared cart+variant key was dropped: one `Place` could delete the other call's id, or `cart.validate` could read the other design. Nothing is written if `AddItem` does not save the line. `CartPort` has no `DiscardEmptyGuestCart`: a failed add after `EnsureStorefrontCart` can leave an empty guest cart and its cookie.
- `UpdatedBy` is the cart customer id when `CartView.CustomerID` is set, otherwise `guest`. `cartExtensionUpdatedBy` returns a non-empty explicit actor before `customerID`, so a hardcoded `guest` would stick on a signed-in cart (`TestPlace_UpdatedByFollowsCartCustomer`). This route's bearer is still the owner token, so the usual cart is a guest cart.
- Custom-tee check is one `FindVariants` batch per cart. Empty `ProductSlug` is not the custom tee.
- An unexpected `cart.validate` payload returns an error and is logged as `metaldogs.cart.validate.payload`. It is not a silent skip. A missing catalog or cart port on a non-empty cart also returns an error.
- Signed-in identity (PR-1554, read in `pkg/pluginsdk/port_adapt.go`): `EnsureStorefrontCart` calls `EnsureActive` when `CustomerID(r)` is set, and does not put that cart id in the guest cookie. A Metal Dogs owner token is dotless base64url. `AuthMiddleware` with `auth.NewValidatingTokenParser` treats it as a guest (`TestAuthMiddleware_DotlessOwnerTokenIsGuest`), so this route still uses the guest cookie. No E2 customer-cart fix beyond that.

## What was not moved

- `cmd/api/metaldogs_catalog.go` still builds catalog repositories for `metaldogs:catalog-seed`.
- Catalog seed, jobs, and media in `plugins/metaldogs` still import `internal/domain` and, for `job_fence.go`, infrastructure. The cart path does not.
- Test imports of `internal/domain` under `plugins/metaldogs` (`owner_token_auth_test.go`, `cart_harness_test.go`) are outside that cart-path line. The owner-token test has to construct `auth.NewValidatingTokenParser`, whose customer and identity types live in `internal/domain`. A Metal Dogs file under `cmd/api` is the same acceptance line, so the test stays next to the plugin.

## Verification

| Item | Result |
| --- | --- |
| `TASK_DB=shopanda_task_mig1 bash ~/ai-os/config/tests/metal_dogs.sh` | Re-run after the review fix: `go test ./plugins/metaldogs/... ./cmd/api/...` exit 0 (about 30s). gofmt passed. ImportBoundary: no new violations; the only red file is `job_fence.go` from the base. `check-upstream-deviations.sh` compares the pin to HEAD, so it passes on this commit (the deleted cart files match the pin once they are gone from HEAD) |
| E2 cart tests (`cart_add_test.go`, `cart_add_rules_test.go`, `cart_validation_test.go`, `cart_design_field_test.go`) | Same assertions, through `CartPort` |
| Known red on the base, not fixed | ImportBoundary: no new violations; the only red file is `job_fence.go` from the base. Also `store_credit_ledger` uuid, notification dedupe, OpenAI smoke |
| Flaky on the base | `TestGenerateWorker_OverlapProcess_WinnerStaysSucceeded` (not run as the only signal) |

## Tests added

- `plugins/metaldogs/cart_place_test.go` — write grant only on `AddItem`; same design and other design do not call `AddItem`, `UpsertLineExtension`, or `DeleteLineExtension`; empty `ProductSlug` does not call `EnsureStorefrontCart`; `UpdatedBy` follows the cart customer; two interleaved `Place` calls on the same variant each keep their own design id
- `plugins/metaldogs/cart_validate_test.go` — unexpected payload errors and is logged; unwired catalog errors; empty `ProductSlug` is not a tee; pending design id is `design_not_found` when the design store is absent
- `plugins/metaldogs/owner_token_auth_test.go` — dotless owner token, production parser, guest, not 401

## Flags / migrations / rollback

- No new migration and no new flag.
- Rollback: revert the change. Cart lines already written keep `metaldogs.design_id`.

## Documentation updated

- [x] `docs/ROADMAP.md` extension-write row and E2 docs line
- [x] `docs/architecture/overview.md` deviation table and pin paragraph
- [x] `docs/architecture/api.md` grant, signed-in cart, validate fail-closed, no pre-write
- [x] `docs/architecture/flows/design-to-order.md` sequence step
- [x] `docs/UPSTREAM_PIN.md`
- [x] `docs/cr-rounds/README.md` links this record
- [x] `.github/scripts/check-upstream-deviations.sh` drops the cart files
