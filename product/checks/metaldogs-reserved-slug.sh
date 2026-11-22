#!/usr/bin/env bash
# --force runs only after the overlay line is present. A missing line would overwrite plugins/metaldogs.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OVERLAY="${ROOT}/product/reserved-slugs.txt"
INIT="${ROOT}/scripts/product-init.sh"

if ! grep -qx 'metaldogs' "${OVERLAY}"; then
  echo "FAIL: product/reserved-slugs.txt has no metaldogs line" >&2
  exit 1
fi

hash_tree() {
  find "${ROOT}/plugins/metaldogs" -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | awk '{print $1}'
}

before="$(hash_tree)"
if bash "${INIT}" --slug metaldogs --force >/dev/null 2>&1; then
  echo "FAIL: product-init --slug metaldogs --force exited 0" >&2
  exit 1
fi
after="$(hash_tree)"
if [[ "${before}" != "${after}" ]]; then
  echo "FAIL: plugins/metaldogs changed under refused --force" >&2
  exit 1
fi
echo "metaldogs reserved slug OK"
