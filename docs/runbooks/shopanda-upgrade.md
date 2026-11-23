# Runbook: upgrading the pinned Shopanda version

- **Introduced by slice:** `F0`
- **Last exercised:** 2026-11-22 (pin → `c25f3e7e`; PR-1599 / shopanda#653, including PR-1598 / shopanda#651 and PR-1597 / shopanda#650). Explicit SHA. Tip of `upstream/dev` and an ancestor of `upstream/main`. Tree equals `upstream/main` tip `d0fe14f1` (shopanda#654). This branch first merged `40e0d571` (`5505327e`); the recorded pin is `c25f3e7e`.
- **Severity when triggered:** medium (planned change with regression risk)
- **Owner:** backend maintainer

## Symptom / trigger

A new upstream commit (or future tag) with security fixes, a needed feature, or a change to an extension point we use. Upgrades are planned slices, never done ad hoc.

## Remotes and pin

| Remote | URL | Role |
| --- | --- | --- |
| `origin` | `https://github.com/akarso/metal_dogs.git` | This product repository (push here) |
| `upstream` | `https://github.com/akarso/shopanda.git` | Shopanda source; fetch-only for upgrades |

Current pin: [`docs/UPSTREAM_PIN.md`](../UPSTREAM_PIN.md).

Do **not** merge from a local Shopanda working tree. Always `git fetch upstream` from GitHub so WIP checkouts elsewhere cannot contaminate Metal Dogs.

**Shallow clone prerequisite:** if `.git/shallow` exists, run `git fetch --unshallow upstream` (or re-clone with full history) **before** merging. A shallow pin hides parent history and makes conflict diagnosis and `git merge-base` unreliable.

## Impact

Risk of contract drift in extension points (cart hook, checkout step, extension fields, events, job queue / sync jobs, storage port, shipment updater) and in migrations ordering. Metal Dogs owns `docs/`; upstream `docs/` changes must not overwrite product docs.

## Procedure

1. Read upstream commit range / release notes for `UPSTREAM_PIN..upstream/main` (or chosen SHA). List any change touching `plugin.App`, `plugin.Migrator`, `pluginsdk` sync jobs, events, extension fields, media storage, checkout workflow, shipment updater.
2. Create a branch `upgrade/shopanda-<shortsha>`.
3. Fetch and merge the pin candidate. `.gitattributes` `merge=ours` does nothing until this clone defines the driver; otherwise git text-merges those paths:

   ```bash
   git config merge.ours.driver true
   git fetch upstream
   git merge <sha>   # prefer an explicit SHA until upstream tags exist
   ```

   Resolve conflicts using the upstream runbook at the pin
   ([product-fork-upgrade](https://github.com/akarso/shopanda/blob/c25f3e7ead48ff75c53667ec7de3a9ab3f7050df/docs/runbooks/product-fork-upgrade.md);
   manifest [`../PRODUCT_OWNED.md`](../PRODUCT_OWNED.md)):
   - **`docs/`** — keep Metal Dogs narrative. Keep upstream `docs/PRODUCT_OWNED.md`. Do not vendor Shopanda guide files, phase history, or the upstream product-fork-upgrade runbook (it links guide files that are not in this tree).
   - **`AGENTS.md`** — keep Metal Dogs project rules; refresh the build/test command section from upstream if commands changed.
   - **`cmd/api/register_products.go`** — keep the product registration (owned). Do not add product plugins to `register_plugins.go`.
   - **Hook rows and other upstream-owned files** (`Makefile`, `docker-compose.yml`, `.env.example`, `configs/config.example.yaml`, `scripts/ci/docs-policy.sh` and its seed copy, `scripts/product-init/reserved-slugs.txt`, `scripts/product-init/product-init_test.sh`) — take upstream. Save the product text first; do not drop it.
   - **`.gitignore`** — take upstream, then `bash scripts/ci/apply-product-gitignore.sh`. Product lines live in `.gitignore.product`. Without that file the script clears the marker section.
   - **`.github/scripts/*` and `.cursor/rules/*`** — not in the manifest. `run-integration-tests.sh` stays the upstream wrapper (`scripts/ci/integration.sh`). Keep the other product scripts and the cursor rule, and list them under upstream deviations.
   - Anything else: prefer upstream unless it is still listed under "Upstream deviations" in `docs/architecture/overview.md`.
4. Update the pin **everywhere** (not only `UPSTREAM_PIN.md`). Pin SHAs are hardcoded in product-facing markdown that links upstream guides. Use a portable rewrite (no GNU `sed -i` / `xargs -r`):

   ```bash
   OLD=<previous-sha>
   NEW=<new-sha>
   python3 -c '
   import pathlib, re, sys
   old, new = sys.argv[1], sys.argv[2]
   pat = re.compile(r"(github\.com/akarso/shopanda/(?:blob|tree)/)" + re.escape(old))
   for path in pathlib.Path(".").rglob("*.md"):
       if ".git" in path.parts:
           continue
       text = path.read_text(encoding="utf-8")
       text2, n = pat.subn(r"\g<1>" + new, text)
       if n:
           path.write_text(text2, encoding="utf-8")
           print(f"updated {path} ({n})")
   ' "$OLD" "$NEW"

   # Also edit docs/UPSTREAM_PIN.md pin table by hand (subject/date).
   # Must be empty for blob/tree URLs:
   test -z "$(rg -l "github.com/akarso/shopanda/(blob|tree)/${OLD}" -g '*.md' || true)"

   # Update PREVIOUS_PIN_FULL_SHA in .github/scripts/check-product-docs-links.sh to
   # the just-replaced OLD full SHA (prose leftover guard; abbreviated history OK).
   ```

   Refresh `docs/guides/README.md` and `docs/diagrams/README.md` if they still mention the old SHA. Re-run `bash .github/scripts/check-product-docs-links.sh` (fails on leftover previous full SHA and bare `guides/` hrefs).
5. Build and test:

   ```bash
   go build -o app ./cmd/api
   go test ./plugins/ -run ImportBoundary -count=1
   bash .github/scripts/check-product-docs-links.sh
   # then the full suite / contract tests for extension points we use
   ```
6. Run plugin migrations against a copy of the production schema; confirm expand/contract compatibility if workers may run mixed versions (`./app migrate`).
7. Deploy to staging; run the E2E flow with the fake provider; run the game-day subset for snapshots and fulfilment.
8. Update `docs/ROADMAP.md §1` if any inspected capability changed; update ADR statuses if a decision is affected.
9. Deploy with the usual rollback plan (previous image; migrations are compatible by rule).

**Named follow-up (not this runbook alone):** GHCR / release image name is still `shopanda` in inherited workflows — change it before the first Metal Dogs version tag so product images are not published as `ghcr.io/<owner>/shopanda`.

## Verification

- `./app dev` serves the default Shopanda storefront (and `/api/customizer/enabled` `200` once `F1` enables the plugin).
- Import-boundary test green; queue depth normal. An end-to-end order on staging produces a snapshot only from E4b, with `plugins.metaldogs.order_snapshot.enabled` true. E4a leaves the flag false and does not snapshot a real order.

## Prevention / follow-up

- Keep deviations from upstream minimal and listed in `docs/architecture/overview.md`.
- Contract tests fail loudly on extension-point drift; add one whenever a new extension point is used.
- Never use `/path/to/local/shopanda` as `upstream` or as a merge source.
