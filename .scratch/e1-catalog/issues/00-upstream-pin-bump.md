# 00: Bump Shopanda pin to expose public price and stock

**What to build:** Move the Metal Dogs pin from `a8cbcb18` to upstream/main `43f0a1ed22b09e5e7a4e7d94f2f74fdd57d4a01b` so public product reads expose catalog price and on-hand stock (upstream commit 479e4c44, PR #573). Separate branch and PR, not part of E1.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] Branch `upgrade/shopanda-43f0a1ed` follows `docs/runbooks/shopanda-upgrade.md` (fetch upstream, explicit SHA merge, conflicts resolved per runbook)
- [ ] Pin updated everywhere (`UPSTREAM_PIN.md`, markdown links, `PREVIOUS_PIN_FULL_SHA`), link check passes
- [ ] Metal Dogs-only deviations (`BoundQueue`, sync-handler request_id, plugin registration) survive; `check-upstream-deviations.sh` passes
- [ ] Build, import-boundary test and full test suite pass; evidence recorded (command, exit code, revision, log path)
- [ ] Findings section lists the public response shape of price and stock on product and variant reads
