#!/usr/bin/env bash
# docs-policy.sh runs every product/checks/*.sh. CI calls these two scripts on its own.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
bash "${ROOT}/.github/scripts/check-product-docs-links.sh"
bash "${ROOT}/.github/scripts/check-upstream-deviations.sh"
