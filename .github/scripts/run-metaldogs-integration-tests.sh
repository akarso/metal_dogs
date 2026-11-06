#!/usr/bin/env bash
# Metal Dogs–only DSN-gated integration suite (F2 harness canaries).
# Kept separate from run-integration-tests.sh so Shopanda package reds cannot
# block F2 canary proof. Requires: go, jq, SHOPANDA_TEST_DSN (non-empty).
set -euo pipefail

if [[ -z "${SHOPANDA_TEST_DSN:-}" ]]; then
  echo "ERROR: SHOPANDA_TEST_DSN is empty or unset" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq is required to parse go test -json output" >&2
  exit 1
fi

pkgs=(
  ./plugins/metaldogs/...
)

json="$(mktemp)"
trap 'rm -f "$json"' EXIT

coverprofile="${COVERPROFILE:-}"
cover_args=()
if [[ -n "$coverprofile" ]]; then
  cover_args=(-coverprofile="$coverprofile" -covermode=atomic)
fi

set +e
go test -p 1 -count=1 "${cover_args[@]}" -json "${pkgs[@]}" >"$json"
status=$?
set -e

if [[ "$status" -ne 0 ]]; then
  echo "ERROR: go test failed with exit $status" >&2
  jq -r 'select(.Action=="fail") | "FAIL \(.Package) \(.Test // "")"' "$json" >&2 || true
  exit "$status"
fi

skip_count="$(jq -sr '[.[] | select(.Action=="skip" and .Test != null)] | length' "$json")"
if [[ "$skip_count" -ne 0 ]]; then
  echo "ERROR: $skip_count test(s) skipped while SHOPANDA_TEST_DSN is set:" >&2
  jq -r 'select(.Action=="skip" and .Test != null) | "SKIP \(.Package) \(.Test)"' "$json" >&2
  jq -r 'select(.Action=="output" and (.Output|test("SHOPANDA_TEST_DSN|--- SKIP"))) | .Output' "$json" >&2 || true
  exit 1
fi

canaries=(
  "github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres|TestMetaStore_RoundTrip"
  "github.com/akarso/shopanda/plugins/metaldogs/testutil|TestOpenIsolated_PoolSeesSchema"
)
for c in "${canaries[@]}"; do
  pkg="${c%%|*}"
  test="${c##*|}"
  if ! jq -se --arg p "$pkg" --arg t "$test" \
    'any(.[]; .Action=="pass" and .Package==$p and .Test==$t)' "$json" >/dev/null; then
    echo "ERROR: canary DSN test did not pass: $pkg $test" >&2
    exit 1
  fi
done

pass_tests="$(jq -sr '[.[] | select(.Action=="pass" and .Test != null)] | length' "$json")"
echo "Metal Dogs integration suite OK: $pass_tests test(s) passed, 0 skipped (go test -p 1)."
