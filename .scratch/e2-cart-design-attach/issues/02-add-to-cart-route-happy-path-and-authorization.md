# 02: Add-to-cart route, happy path and authorization

**What to build:** A customer with an approved design adds it to the Shopanda cart in a chosen size through one Metal Dogs route and learns where the cart page is. The route reaches the cart through a narrow port supplied by the composition root, writes the design field with a trusted plugin write, and uses the customer's session cart (created when missing). A design that is not theirs, unknown, expired, not approved, or a variant outside the custom tee is refused with the agreed errors.

**Blocked by:** 01.

**Status:** ready-for-agent

- [ ] `POST /api/customizer/designs/{id}/add-to-cart` with the owner token and a variant of the custom tee adds one line, quantity one, with `metaldogs.design_id` equal to the design id, visible in the public cart API response
- [ ] The response contains the target for the Shopanda cart page and no raw cart id
- [ ] Works for a guest: the session cart cookie is used, and a cart is created when none exists
- [ ] Foreign token, unknown id and expired design return 404; a design that is not `approved` returns 409; a variant outside the custom tee and an invalid request return 422
- [ ] Token in the query string is rejected like in the other design routes
- [ ] The design is not modified (status, versions and expiry unchanged)
- [ ] The cart port comes from the composition root; the trusted-write marker and field code are fixed in code and never read from request input; the new composition file is on the upstream-deviations allowlist and `check-upstream-deviations.sh` passes
- [ ] The Shopanda core is not modified; comments follow the AGENTS.md "Code comments" rules
