#!/usr/bin/env bash
# Paths that differ from the pinned upstream SHA must be allowlisted here
# (keep in sync with docs/architecture/overview.md).
# Product-owned trees are excluded: plugins/metaldogs, docs, markdown,
# .github/workflows, .scratch.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

PIN="$(grep -E '^\| Pinned commit \|' docs/UPSTREAM_PIN.md | grep -oE '[0-9a-f]{40}' | head -n1 || true)"
if [[ ! "${PIN}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "ERROR: could not parse 40-char pin from docs/UPSTREAM_PIN.md" >&2
  exit 1
fi

# Every path git reports outside the product-owned trees above.
# Reasons match docs/architecture/overview.md. '#' comments are not part of the path.
ALLOWLIST=(
  .cursor/rules/pr-documentation-checklist.mdc # Metal Dogs main-only / CR-round checklist
  .env.example # private media path, METALDOGS_ENABLED, compose DSN
  .github/scripts/check-product-docs-links.sh # product docs links; previous-pin SHA guard
  .github/scripts/check-upstream-deviations.sh # this allowlist
  .github/scripts/run-integration-tests.sh # Shopanda DSN suite; Metal Dogs canaries are separate
  .github/scripts/run-metaldogs-integration-tests.sh # Metal Dogs DSN suite and canaries
  .gitignore # /app, .idea/, .vscode/
  Makefile # docs-policy runs the product link checker and this script
  cmd/api/plugin_boot.go # plugin CLI skips CapJobs and CapMedia; those ports are not bound for templates or catalog-seed
  cmd/api/register_plugins_metaldogs_test.go # registration, NoCache, boot, catalog-seed CLI, opaque bearer, migrator collect
  cmd/api/register_products.go # registerMetalDogs when enabled or the flag does not parse
  configs/config.example.yaml # commented plugins.metaldogs block
  docker-compose.yml # Postgres on 127.0.0.1 and SHOPANDA_DATABASE_HOST_PORT
  scripts/ci/docs-policy.sh # product link checker; skip promote fixture unless SHOPANDA_PLATFORM=1
  scripts/product-init/product-init_test.sh # --force refuses slug metaldogs
  scripts/product-init/reserved-slugs.txt # metaldogs reserved so --force cannot clobber it
  scripts/product-init/seed/scripts-ci/docs-policy.sh # seed copy of the product docs-policy.sh
  storefront/.gitkeep # empty storefront dir; product app is not the upstream theme tree
)

mapfile -t dirty < <(
  git diff --name-only "${PIN}" HEAD -- . \
    ':!plugins/metaldogs' \
    ':!docs' \
    ':!.github/workflows' \
    ':!.scratch' \
    ':!*.md' \
    | sort -u
)

unlisted=()
for path in "${dirty[@]}"; do
  [[ -z "${path}" ]] && continue
  allowed=0
  for a in "${ALLOWLIST[@]}"; do
    if [[ "${path}" == "${a}" ]]; then
      allowed=1
      break
    fi
  done
  if [[ "${allowed}" -eq 0 ]]; then
    unlisted+=("${path}")
  fi
done

stale=()
for a in "${ALLOWLIST[@]}"; do
  found=0
  for path in "${dirty[@]}"; do
    if [[ "${path}" == "${a}" ]]; then
      found=1
      break
    fi
  done
  if [[ "${found}" -eq 0 ]]; then
    stale+=("${a}")
  fi
done

if [[ "${#unlisted[@]}" -gt 0 || "${#stale[@]}" -gt 0 ]]; then
  if [[ "${#unlisted[@]}" -gt 0 ]]; then
    echo "ERROR: paths differ from pin ${PIN} but are not on the absorb allowlist:" >&2
    printf '  %s\n' "${unlisted[@]}" >&2
  fi
  if [[ "${#stale[@]}" -gt 0 ]]; then
    echo "ERROR: allowlist entries do not differ from pin ${PIN}:" >&2
    printf '  %s\n' "${stale[@]}" >&2
  fi
  echo "Update docs/architecture/overview.md and this script together." >&2
  exit 1
fi

echo "upstream-deviations OK (pin ${PIN:0:12}, ${#ALLOWLIST[@]} allowlisted path(s))"
