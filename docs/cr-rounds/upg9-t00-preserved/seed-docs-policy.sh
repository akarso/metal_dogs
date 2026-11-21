#!/usr/bin/env bash
# Product-doc link policy (+ optional promote fixture).
#
# Usage:
#   bash scripts/ci/docs-policy.sh
#
# Metal Dogs (this tree): prefer .github/scripts/check-product-docs-links.sh and
# skip the promote fixture unless SHOPANDA_PLATFORM=1 (upstream platform CI only).
#
# SHOPANDA_PLATFORM=1:
#   fail closed if the promote fixture is missing or no product-doc roots exist.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

export SHOPANDA_PLATFORM="${SHOPANDA_PLATFORM:-0}"

if [[ -f "${ROOT}/.github/scripts/check-product-docs-links.sh" ]]; then
  bash "${ROOT}/.github/scripts/check-product-docs-links.sh"
else
  bash "${ROOT}/scripts/ci/check-product-docs-links.sh"
fi

if [[ "${SHOPANDA_PLATFORM}" == "1" ]]; then
  if [[ ! -f "${ROOT}/scripts/promote-dev-to-main_test.sh" ]]; then
    echo "ERROR: scripts/promote-dev-to-main_test.sh missing (required on Shopanda platform CI)" >&2
    exit 1
  fi
  bash "${ROOT}/scripts/promote-dev-to-main_test.sh"
elif [[ -f "${ROOT}/scripts/promote-dev-to-main_test.sh" ]]; then
  echo "NOTE: skipping promote fixture on product tree (set SHOPANDA_PLATFORM=1 only on upstream)."
fi

echo "docs-policy OK"
