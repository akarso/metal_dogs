# ADR-0007: Guest designs are protected by per-design capability tokens

- **Status:** Proposed (to be Accepted at close of `C3`)
- **Date:** 2026-10-15
- **Related slices:** `C3`, `C4`, `C5`, `C6`, `E2`, `S2`, `S3`
- **Related ADRs:** [ADR-0002](0002-storefront-customizer-and-shopanda-checkout.md), [ADR-0004](0004-private-object-storage-signed-urls.md)

## Context

MVP has no customer accounts (out of scope unless required for sales). Shopanda supports guest checkout. `AGENTS.md` requires authorization on every design, job, and asset, including guest-owned resources, and the project description asks for "secure, hard-to-guess tokens for guest designs".

## Decision

1. `POST /api/customizer/designs` returns, **once**, an `owner_token`: 256 bits from a CSPRNG, base64url. The server stores only `SHA-256(owner_token)`.
2. Every design-scoped request (`GET/PATCH /designs/{id}`, `/generate`, `/generation-jobs/{id}`, `/approve`, `/add-to-cart`) must present the token in `Authorization: Bearer <owner_token>`. Mismatch or unknown design → `404` (not `403`, to avoid confirming existence).
3. Generation job lookups are authorized through the owning design's token.
4. The customizer stores `design_id` + token in `sessionStorage` (cleared when the tab closes) and offers "continue later" only if a slice adds email-based recovery; no token in URLs.
5. When the design is added to the cart, the cart hook verifies the token; from then on, the **cart session** owns the line and Shopanda's checkout authorization applies. The order snapshot does not depend on the token.
6. If customer accounts are introduced (`X4`), `customer_id` is set on the design at creation when the customer is logged in, and either the token or the session authorizes; tokens are not removed.
7. Tokens are never logged; the hash is never returned.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Tie designs to the Shopanda guest session cookie | Couples the plugin to session internals; breaks when the cookie resets mid-flow; harder to test |
| Unguessable design ids only | An id in a log line or URL becomes a credential |
| Require accounts | Out of scope; hurts conversion |
| Signed JWT per design | More machinery for the same guarantee; stateless revocation not needed |

## Consequences

- Positive: simple, stateless on the client, auditable, independent of Shopanda session internals.
- Negative: losing the tab loses the design unless resumed from storage. Acceptable for MVP; mitigation via `sessionStorage` and retention window.

## Compliance

- Feature tests: missing/wrong token → `404`; token in query string rejected; token never appears in logs (log capture assertion).
- Unit test: token entropy ≥ 256 bits; only hash persisted.
