# CR-31: Shopanda pin `c25f3e7e` (UPG10)

- Opened: 2026-11-22
- Pin: `c25f3e7e…` (shopanda#653, PR-1599; includes shopanda#651 / PR-1598 and shopanda#650 / PR-1597)
- Previous published pin: `72785f27…` (shopanda#648, PR-1596)
- Intermediate merge on this branch: `40e0d571` (shopanda#651) in commit `5505327e`. That SHA is not the pin. `TestPublicProductVisibilityRoutes_SyncWithWireRoutes` failed on it because `GET /checkout/payment/return` was missing from the visibility list. PR-1599 adds that row.
- `upstream/dev` tip at fetch: `c25f3e7e` (this pin).
- `upstream/main` tip at fetch: `d0fe14f1` (shopanda#654). This pin is an ancestor of that tip. The three trees are equal.

Merge-base of `5505327e` and this pin is `40e0d571`. Merge-base of this branch and `72785f27` is `72785f27`.

CR-30 is not this bump. E3's record is not in this branch.

## Ticket 00 — pin `c25f3e7e`

- Date: 2026-11-22

### Where the pin sits

`c25f3e7e` is the tip of `upstream/dev` and an ancestor of `upstream/main`. It is on both. It is not only on `dev`. `git rev-parse <pin>^{tree}`, `upstream/dev^{tree}`, and `upstream/main^{tree}` are the same object (`7d330fb4`).

### Two-step merge

1. `5505327e` merged `40e0d571` (PR-1598, including PR-1597). Conflicts from that step are in the first table below.
2. This merge brings `a866a2b1` and `c25f3e7e` (PR-1599) onto that branch and re-points every pin reference to `c25f3e7e`.

### Conflict resolution — second merge (`40e0d571` → `c25f3e7e`)

| File | Kept |
| --- | --- |
| `docs/phase-17-product-packaging/ROADMAP.md` | Deleted (modify/delete) |
| `docs/phase-17-product-packaging/prs/PR-1599.md` | Deleted (new upstream PR note) |

No conflict under `internal/`, `pkg/`, or `cmd/api`. `internal/interfaces/http/storefront/visibility_boundary.go` and `visibility_boundary_test.go` match the pin (the return route row).

`.env.example` and `configs/config.example.yaml` are unchanged in this range and match the pin. Product lines stay in `.env.example.product` and `configs/config.example.product.yaml`. Nothing was copied into those companions. No new deviation row.

`themes/default/templates/checkout_confirm.html` and `themes/default/templates/checkout_payment.html` are unchanged in this range. The product does not override them (no other theme tree). They match this pin. There was no theme conflict to keep.

### Conflict resolution — first merge (`72785f27` → `40e0d571`)

| File | Kept |
| --- | --- |
| `AGENTS.md` | Metal Dogs (`merge=ours`). Upstream added two plugin-recipe rows (checkout client code, SSR Stripe return). That table is not in the product file. Build and test commands did not change. |
| `RUNBOOK.md` | Metal Dogs (`merge=ours`). Upstream added one sentence to the downloadable-grant Stripe check. That section is not in the product file. Pin blob links point at this SHA. |
| `docs/guides/DEPLOYMENT.md` | Deleted (modify/delete). Stub `docs/guides/README.md` stays. The Stripe publishable-key note is at the pin on GitHub. |
| `docs/phase-17-product-packaging/ROADMAP.md` | Deleted (modify/delete) |
| `docs/phase-17-product-packaging/prs/PR-1597.md` | Deleted (new upstream PR note) |
| `docs/phase-17-product-packaging/prs/PR-1598.md` | Deleted (new upstream PR note) |

No conflict under `internal/`, `pkg/`, or `cmd/api` in that step. Those paths match the pin except the allowlisted product files `cmd/api/register_products.go` and `cmd/api/register_plugins_metaldogs_test.go`.

`docs/ROADMAP.md` and `docs/architecture/overview.md` did not contain the sentences "SDK has no plugin error code/details" or "SSR checkout does not confirm Stripe" (those notes are on the unmerged E3 branch). §1 states the pin fact: checkout JSON can carry `plugin.<slug>`, `details`, and HTTP 503 (PR-1597), SSR checkout confirms a Stripe PaymentIntent (PR-1598), and `GET /checkout/payment/return` is out-of-scope in `PublicProductVisibilityRoutes` (PR-1599). Neither file records a Stripe test-mode run. `plugins/metaldogs` is unchanged and does not call `WithClientCode`.

### Allowlist after this bump

Same sixteen paths as [CR-29](CR-29-upg9-pin-72785f27.md) ticket 01. No path was added.

### Upstream range `72785f27..c25f3e7e`

First step (`72785f27..40e0d571`): `478c643a` (client code and details), `8e7cfc30` (SSR Stripe confirm), `c5679e8e` (SCA harden), `0e6298bd` (apperror tests), `b51a5d2c` (flash and `return_url`), `223bdda3` (forged flash), `d948f0c6` (details copy wording), `91e3a8ed` (merge shopanda#650), `40e0d571` (merge shopanda#651).

Second step (`40e0d571..c25f3e7e`): `a866a2b1` (list `GET /checkout/payment/return` under product visibility out-of-scope), `c25f3e7e` (merge shopanda#653).

- `pkg/pluginsdk`: `WithClientCode` (`plugin.<slug>`), `WithDetails`, `UnavailableError`, `IsUnavailable`.
- `internal/platform/apperror` and `internal/platform/httpx`: `ClientCode`, `Details`, `CodeUnavailable` maps to 503. `httpx.ErrorBody.Details`.
- `internal/interfaces/http/storefront`: SSR confirm renders `checkout_confirm`, POST then 303 then GET, `GET /checkout/payment/return`. `payment.stripe.publishable_key`. The return route is in `PublicProductVisibilityRoutes` and the contract table (PR-1599).
- `themes/default/templates/checkout_confirm.html` and `checkout_payment.html`.
- `.env.example` and `configs/config.example.yaml`: commented Stripe keys, including the publishable key.
- `cmd/api/wire_routes.go`: return route (from the first step).

No change in this range to `plugin.App` registration, `plugin.Migrator`, cart, extension fields, media storage, events, or the shipment updater. The range does change checkout workflow, checkout API errors, and the visibility list. Metal Dogs does not use the new error fields (E3 ticket 04).
