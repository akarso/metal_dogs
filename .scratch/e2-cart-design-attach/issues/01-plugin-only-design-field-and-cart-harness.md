# 01: Plugin-only design field and cart test harness

**What to build:** The plugin registers the cart-line field `metaldogs.design_id` so that only Metal Dogs code can set it, and the project gets a test harness that runs the real storefront and cart on an isolated database with the custom tee published and in stock. A client that tries to set the field through the generic cart API is refused and nothing is stored.

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] Field `metaldogs.design_id` is registered by the plugin when it is enabled: public, plugin-only write, snapshot storage, UUID-shaped validation
- [ ] Through the public cart API only: adding a line with that field returns 403 and no line is created with it; updating a line with it returns 403 and nothing changes
- [ ] A normal add of the custom tee without the field through the generic API still works (the block is only on the field)
- [ ] Harness: real storefront and cart over an isolated database with core and plugin migrations, catalog seeded with the E1 command, product published and stock set by the test; the E1 seed files are not changed
- [ ] With the plugin disabled the field is not registered (existing wiring test style)
- [ ] The Shopanda core is not modified; comments in new code follow the AGENTS.md "Code comments" rules
