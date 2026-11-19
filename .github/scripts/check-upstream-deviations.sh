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
ALLOWLIST=(
  .cursor/rules/pr-documentation-checklist.mdc
  .env.example
  .github/scripts/check-product-docs-links.sh
  .github/scripts/check-upstream-deviations.sh
  .github/scripts/run-integration-tests.sh
  .github/scripts/run-metaldogs-integration-tests.sh
  .gitignore
  Makefile
  cmd/api/migrate_plugins_test.go
  cmd/api/register_plugins_metaldogs_test.go
  cmd/api/register_products.go
  configs/config.example.yaml
  docker-compose.yml
  scripts/ci/docs-policy.sh
  scripts/product-init/product-init_test.sh
  scripts/product-init/reserved-slugs.txt
  scripts/product-init/seed/scripts-ci/docs-policy.sh
  storefront/.gitkeep
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
