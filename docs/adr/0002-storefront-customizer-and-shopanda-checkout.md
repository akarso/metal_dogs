# ADR-0002: Custom vanilla-JS customizer; Shopanda SSR for cart and checkout

- **Status:** Proposed (to be Accepted at close of `S1` and `E3`)
- **Date:** 2026-10-15
- **Related slices:** `S1`–`S6`, `E2`, `E3`
- **Related ADRs:** [ADR-0001](0001-metal-dogs-as-shopanda-external-plugin.md), [ADR-0007](0007-guest-design-ownership-tokens.md)

## Context

The project description prescribes HTML, CSS, and vanilla JavaScript for the storefront, mobile-first, with no framework or canvas library unless an interaction requires it, and says Shopanda is used "in headless mode". Shopanda (verified) ships an SSR storefront with themes, UI slots, and an asset manifest, plus guest checkout and a Stripe plugin.

Building our own cart, checkout, address, payment, and order-confirmation pages would duplicate commerce features that `AGENTS.md` says to reuse.

## Decision

1. The **customizer** (genre → template → texts → photo → generation → preview → approve → size) is a standalone vanilla HTML/CSS/JS application under `storefront/`, served from the **same origin** as Shopanda at `/customize` (via a plugin public route or the reverse proxy; chosen in `S1`).
2. **Cart, checkout, payment, and order pages are Shopanda's SSR pages.** After "add to cart" the customer is redirected to Shopanda's cart. The only customisation of those pages is optional: a design thumbnail in the cart line and confirmation via UI slots, if the slot API supports it.
3. The customizer talks only to `/api/customizer/*`. It never calls the AI provider, storage, or POD directly and holds no credentials.
4. "Headless" is interpreted as: the customizer is a client of the plugin's REST API; it does not mean every page must be custom.
5. No framework, bundler, or canvas library is introduced until a slice demonstrates the need. Preview is composed server-side (G4); the client only displays images.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Fully custom headless storefront for all pages | Rebuilds commerce UI, delays MVP, duplicates guest-checkout and payment handling |
| Customizer as a Shopanda theme page using UI slots only | Slots inject HTML at fixed anchors; the multi-step flow needs its own page and state; would still need the API |
| React/Vue SPA | Explicitly excluded by the project description for MVP |
| Client-side mockup composition on canvas | Server-side composition is deterministic, testable with golden images, and keeps the preview identical to what the operator sees |

## Consequences

- Positive: smallest surface; same-origin cookies make the guest cart session work without CORS; checkout security stays with Shopanda/Stripe.
- Negative: visual seam between the customizer and Shopanda's theme. Mitigation: shared CSS tokens via the asset manifest; theme adjustments in `S6`.
- Negative: two UI codebases (customizer + theme). Mitigation: keep theme changes minimal and documented.

## Compliance

- No fetch to a non-same-origin host in `storefront/` (grep check in CI).
- No framework dependency in `storefront/` without a superseding ADR.
