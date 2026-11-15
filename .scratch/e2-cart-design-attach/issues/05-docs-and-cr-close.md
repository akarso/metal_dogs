# 05: Documentation and CR close

**What to build:** Everything a maintainer needs to understand and operate the add-to-cart flow, and the roadmap and review record updated to match what was built.

**Blocked by:** 03, 04.

**Status:** ready-for-agent

- [ ] Customizer API reference documents the add-to-cart route: request, success response, error table (403 on other paths, 404, 409, 422)
- [ ] The design-to-order flow matches the decisions: session cart, no raw cart id, trusted plugin write, snapshot copy at checkout
- [ ] ROADMAP E2 card is corrected ("inert without design id" applies to non-tee products only) and marked done with its CR reference
- [ ] A CR round file following the existing CR-NN convention is added, linked from the rounds index, and lists known gaps (guest cart merge copies the field without hooks; a signed-in customer is written to the guest cookie cart, which `/cart` does not load; rate limit is O4)
- [ ] Docs policy check (`make docs-policy`) passes
- [ ] Full relevant test suite passes and the evidence (command, exit code, revision, log path) is recorded in the CR round file
