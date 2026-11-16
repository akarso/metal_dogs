# 03: Add rules, idempotency and conflicts

**What to build:** The add route behaves predictably when the cart already has the variant: repeating the same request changes nothing, a different design on the same size is refused, the same design in another size becomes a second line, and the customer can raise the quantity in the cart.

**Blocked by:** 02.

**Status:** ready-for-agent

- [ ] Same design, same variant again: success, cart identical, quantity unchanged
- [ ] A different approved design on a variant that already carries a design: 409, cart unchanged
- [ ] The same design on a different variant: allowed, two lines
- [ ] Quantity raised to two or more through the public cart API is accepted; a repeat of the add afterwards leaves it unchanged and the design field intact
- [ ] Removing the design line and adding it again works and the field is set again
- [ ] All cases observed through the route and the public cart API only
- [ ] Comments follow the AGENTS.md "Code comments" rules
