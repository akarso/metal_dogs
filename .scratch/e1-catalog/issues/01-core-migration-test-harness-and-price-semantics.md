# 01: Core-migration test harness and price semantics check

**What to build:** A test harness for the Metal Dogs plugin that gives a test an isolated database with both the Shopanda core migrations and the plugin migrations applied, and proves that the public storefront API can be called against it. As part of the same ticket, the developer confirms whether the core treats a stored price amount as net or gross and records the finding in this ticket.

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] A test can obtain an isolated database with core and plugin migrations applied; it is skipped when no test DSN is configured, like the existing helper
- [ ] A smoke test starts the storefront API against that database and reads an empty product list successfully
- [ ] The isolated schema is dropped on cleanup; running the package twice leaves no leftovers
- [ ] Finding recorded under a `## Findings` heading in this file: stored price amounts are net or gross, with the code that proves it
- [ ] If the core treats stored amounts as gross, work stops and the finding is reported before any other ticket starts (the decision "store net" must be revisited)
- [ ] Existing plugin tests still pass
