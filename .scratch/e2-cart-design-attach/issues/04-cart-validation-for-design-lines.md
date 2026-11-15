# 04: Cart validation for design lines

**What to build:** The cart reports a blocking problem when a custom tee line has no design, or its design is no longer approved, has expired or does not belong to the custom tee. Every other product is unaffected, so checkout (E3) can rely on the cart's validation outcome.

**Blocked by:** 02.

**Status:** ready-for-agent

- [ ] A custom tee line added through the generic API without `design_id` yields a blocking cart validation issue ("design required")
- [ ] A design line whose design is no longer `approved` (changed after the add) yields a blocking issue
- [ ] A design line whose design has expired, or whose design id does not match the custom tee, yields a blocking issue
- [ ] A cart with a valid design line yields no issue from this plugin
- [ ] A cart with another product and no design id is unaffected
- [ ] With the plugin disabled no validation is registered
- [ ] Ownership is not checked here (the add route did it); the hook does not need a token
- [ ] Comments follow the AGENTS.md "Code comments" rules
