# 04: Documentation and ADR close

**What to build:** Everything a future maintainer needs to run, understand and launch the catalog, and the decision record updated to match reality.

**Blocked by:** 03.

**Status:** ready-for-agent

- [ ] Plugin README has a catalog section: data files, the command, accepted price formats, net-to-gross formula, the price-semantics finding from ticket 01
- [ ] Publish-at-M3 steps documented: set real price, load real stock, set status to active; a note that draft status and zero stock are two independent protections
- [ ] ADR-0009: the catalog modelling decision is marked Accepted with an E1 close note; the fulfilment part stays Proposed until P0
- [ ] ROADMAP marks E1 done with its CR reference; a CR round file following the existing CR-NN convention is added and linked from the rounds index
- [ ] Docs policy check (`make docs-policy`) passes
- [ ] Full relevant test suite passes and the evidence (command, exit code, revision, log path) is recorded in the CR round file
