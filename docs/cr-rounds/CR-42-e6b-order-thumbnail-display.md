# CR-42: E6b order thumbnail on the confirmation email and the order pages

- Status: **closed**
- Closed: 2026-11-25
- Slices: `E6b`
- Milestone progress: M4 → E6b. M4 stays open (P2, P3, P4, O3, O5, O6).

## What shipped

The plugin registers hook `order.line.display` and appends one image field for a custom-tee line that has a snapshot thumbnail. Dark. `plugins.metaldogs.order_snapshot.enabled` stays false.

- Registration matches `cart.validate`: `Host.Hooks(p.Name()).Register`, priority 100. The hook is registered only when the plugin is enabled, `order_snapshot.enabled` is true, `thumbnail_signing_key` is set (at least 32 characters), and `public_base_url` is https. Otherwise it is absent. When the flag, the key, and a base URL are set but the base is not https, Init logs `metaldogs.order_line_display.public_base_url` once (`reason=not_https`) and does not register the hook. The thumbnail HTTP route stays the E6a rule (flag and key, including http).
- The handler reads `order_id`, `variant_id`, and `language` and calls `(*Plugin).OrderThumbnailURL`. An empty URL or any error adds no field and logs `metaldogs.order_line_display.skipped` (info) or `metaldogs.order_line_display.failed` (warn) with `order_id` and `variant_id` only. The handler returns nil. The lookup does not require the order to be paid.
- The field is code `metaldogs.thumbnail`, kind image, value the URL. Label and alt come from one table, by BCP 47 primary subtag (`pl-PL` → pl; unknown or empty → en): en "Your design" / "Preview of your custom design"; pl "Twój projekt" / "Podgląd Twojego projektu"; de "Dein Design" / "Vorschau deines individuellen Designs"; fr "Votre création" / "Aperçu de votre création personnalisée"; es "Tu diseño" / "Vista previa de tu diseño personalizado".
- Shopanda renders `Kind=image` on the confirmation email and on both order pages. This round does not edit those templates, and it does not edit `internal/*`, `pkg/*`, or core `cmd/api/*`. No migration.
- `localhost` is not a public host. The platform drops the image, and a non-https local base never registers the hook. Say so in the local-development runbook.

## Decisions

- The string table is only the thumbnail label and alt. The rest of the plugin copy stays untranslated. Proposed slice I1 covers customizer UI and API errors.
- A line with no snapshot and a line for another product are the same skip: `OrderThumbnailURL` is empty. The handler does not classify the product slug.
- The account order page was not rendered from the plugin tests. Storefront fixture helpers are unexported in `storefront_test`, and this round cannot edit `internal/*`. Upstream `TestStorefrontHandler_AccountOrderDetail_OrderLineDisplayImage` already asserts `<img>` for `Kind=image`.

## Verification results

| Item | Result |
| --- | --- |
| `SHOPANDA_TEST_DSN` on `127.0.0.1:5433/shopanda_task_e6b` `go test ./plugins/metaldogs/... -count=1 -p 1` | Exit 0 on 2026-11-25. Includes `TestOrderLineDisplay_HookOnPostgres`, `TestOrderLineDisplay_NotRegisteredWhileDark`, `TestOrderLineDisplay_HTTPBaseWarnsAndSkipsRegistration` |
| `go test ./plugins/ -run ImportBoundary -count=1` | Exit 0. No `imports forbidden` or `imports disallowed` lines |
| `bash .github/scripts/check-product-docs-links.sh` | Exit 0 on 2026-11-25 |

## Tests added

- `plugins/metaldogs/order_line_display_test.go` — `TestOrderLineDisplay_HookOnPostgres`: registry invoke on isolated Postgres; image field equals `OrderThumbnailURL`; languages en, pl, de, fr, es, `pl-PL`, unknown, empty, and an omitted language; no field for a snapshot without a thumbnail, a missing snapshot, and another variant with no snapshot; `GET` on the produced URL returns the thumbnail bytes; a closed database skips, logs order id and variant id, and does not fail the hook. `TestOrderLineDisplay_NotRegisteredWhileDark`: flag off, key absent, empty base, plugin disabled. `TestOrderLineDisplay_HTTPBaseWarnsAndSkipsRegistration`: one warning, hook absent, thumbnail route still registered.

## Flags / migrations / rollback

- `plugins.metaldogs.order_snapshot.enabled` stays default false.
- Hook also needs `thumbnail_signing_key` and an https `public_base_url`.
- No migration. Rollback: flag off, remove the key, or set a non-https base. The E6a route and columns stay.
- Logs: order id and variant id on a skip or a failure. No token, no storage key, no URL.

## Documentation updated

- [x] `docs/ROADMAP.md`: E6b **DONE** (CR-42). M4 note: E6b is done; M4 stays open for P2 P3 P4 O3 O5 O6. I1 proposed for the rest of plugin copy. E6a known limits point here.
- [x] `docs/architecture/api.md`: hook, string table, https gate.
- [x] ADR-0004 amendment: the order-line image field. `docs/adr/README.md` matches.
- [x] `docs/architecture/flows/checkout-snapshot-and-fulfillment.md`: the hook calls `OrderThumbnailURL`.
- [x] `docs/architecture/overview.md`: the pin paragraph records the registration.
- [x] `docs/runbooks/README.md`, `docs/runbooks/local-development.md`, and `docs/runbooks/order-without-design-snapshot.md`: https required; `localhost` does not show the image; a missing snapshot when the confirmation email is rendered leaves that email without the picture.
- [x] `configs/config.example.product.yaml` and `plugins/metaldogs/README.md`: hook and string table.
- [x] [CR-37](CR-37-e6a-order-thumbnail.md) follow-up points here.
- [x] Index row in this file's parent README.
- [x] `AGENTS.md`: no new CLI command. Build and test commands unchanged.

## Follow-ups

- I1: localize customizer UI strings and API error messages. Not scheduled.
- M3 still turns `order_snapshot.enabled` on and sets the signing key and an https `public_base_url`. This round does not.
- A snapshot written before a mockup existed stays without a thumbnail. Nothing backfills it.
