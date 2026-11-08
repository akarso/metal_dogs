#!/usr/bin/env bash
# Fail if product-facing docs use in-repo relative links into docs/phase-*
# or hardcoded GitHub blob/tree URLs into working history on `dev`.
# Working-history paths are named in prose; they must not be markdown hrefs
# on the product surface (those 404 on main after promote / are not vendored here).
#
# Metal Dogs: only scan paths that exist. Upstream guides/diagrams are stubs
# that point at the pinned Shopanda commit (see docs/guides/README.md).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

shopt -s nullglob

# Markdown href to a guides/diagrams *file* (allows #fragment and ?query before ')').
# Stub indexes docs/guides/README.md and docs/diagrams/README.md are allowed.
MD_GUIDE_HREF='\]\((\.\./)*(docs/guides|docs/diagrams)/[^)]+\)'
# Bare guides/… hrefs (e.g. docs/COMMERCIAL.md historically used guides/DEVELOPER.md).
MD_BARE_GUIDE_HREF='\]\(guides/[^)]+\)'
# Backtick path mentions of the same (with or without leading ../).
BT_GUIDE_PATH='`(\.\./)*(docs/guides|docs/diagrams)/[^`]+`'
# Allow stub indexes whether written as docs/guides/README.md or guides/README.md
# (from docs/README.md).
STUB_ALLOW='(docs/)?guides/README\.md|(docs/)?diagrams/README\.md'
# Previous pin full SHA must not linger in product-facing markdown after a pin bump
# (blob/tree rewrite alone misses prose). History may use the abbreviated form.
PREVIOUS_PIN_FULL_SHA='5681894658c44ce344b2df930effecda7ffd43c4'

self_test() {
  local tmp hits
  tmp="$(mktemp)"
  # EXIT covers failure paths that call exit 1; RETURN covers normal return.
  # shellcheck disable=SC2064
  trap "rm -f '${tmp}'" EXIT

  printf '%s\n' '[x](../../docs/guides/DEVELOPER.md#three-tier-extension-model)' >"${tmp}"
  hits="$(grep -E "${MD_GUIDE_HREF}" "${tmp}" | grep -Ev "${STUB_ALLOW}" || true)"
  if [[ -z "${hits}" ]]; then
    echo "SELF-TEST FAIL: fragment guide href must be detected" >&2
    exit 1
  fi

  printf '%s\n' '[ok](docs/guides/README.md)' >"${tmp}"
  hits="$(grep -E "${MD_GUIDE_HREF}" "${tmp}" | grep -Ev "${STUB_ALLOW}" || true)"
  if [[ -n "${hits}" ]]; then
    echo "SELF-TEST FAIL: docs/guides/README.md stub href must be allowed" >&2
    exit 1
  fi

  printf '%s\n' 'See `docs/guides/PLUGIN_COMPOSITION.md`.' >"${tmp}"
  hits="$(grep -E "${BT_GUIDE_PATH}" "${tmp}" | grep -Ev "${STUB_ALLOW}" || true)"
  if [[ -z "${hits}" ]]; then
    echo "SELF-TEST FAIL: backtick guide path must be detected" >&2
    exit 1
  fi

  printf '%s\n' 'See `../../docs/guides/DEVELOPER.md#frag`.' >"${tmp}"
  hits="$(grep -E "${BT_GUIDE_PATH}" "${tmp}" | grep -Ev "${STUB_ALLOW}" || true)"
  if [[ -z "${hits}" ]]; then
    echo "SELF-TEST FAIL: relative backtick guide path must be detected" >&2
    exit 1
  fi

  printf '%s\n' 'See `docs/guides/README.md`.' >"${tmp}"
  hits="$(grep -E "${BT_GUIDE_PATH}" "${tmp}" | grep -Ev "${STUB_ALLOW}" || true)"
  if [[ -n "${hits}" ]]; then
    echo "SELF-TEST FAIL: backtick stub README must be allowed" >&2
    exit 1
  fi

  printf '%s\n' '[x](guides/DEVELOPER.md#frag)' >"${tmp}"
  hits="$(grep -E "${MD_BARE_GUIDE_HREF}" "${tmp}" || true)"
  if [[ -z "${hits}" ]]; then
    echo "SELF-TEST FAIL: bare guides/ href must be detected" >&2
    exit 1
  fi

  printf '%s\n' "pinned ${PREVIOUS_PIN_FULL_SHA}" >"${tmp}"
  hits="$(grep -F "${PREVIOUS_PIN_FULL_SHA}" "${tmp}" || true)"
  if [[ -z "${hits}" ]]; then
    echo "SELF-TEST FAIL: previous pin full SHA must be detectable" >&2
    exit 1
  fi

  trap - EXIT
  rm -f "${tmp}"
}

candidate_paths=(
  README.md
  PLUGINS.md
  RUNBOOK.md
  AGENTS.md
  docs/README.md
  docs/COMMERCIAL.md
  docs/GOVULN_BASELINE.md
  docs/UPSTREAM_PIN.md
  docs/PROJECT_DESCRIPTION.md
  docs/ROADMAP.md
  docs/guides
  docs/diagrams
  docs/runbooks
  docs/architecture
  docs/adr
  docs/cr-rounds
  plugins/*/README.md
)

paths=()
for p in "${candidate_paths[@]}"; do
  if [[ -e "${p}" ]]; then
    paths+=("${p}")
  fi
done

if [[ "${#paths[@]}" -eq 0 ]]; then
  echo "ERROR: no product docs paths to scan" >&2
  exit 1
fi

# Relative links / backtick paths to upstream guide/diagram *files* that are not
# vendored here (stubs live only as docs/guides/README.md and docs/diagrams/README.md).
scan_missing_upstream_docs() {
  local hits ec
  set +e
  hits="$(
    {
      grep -RInE "${MD_GUIDE_HREF}" -- "${paths[@]}" || true
      grep -RInE "${MD_BARE_GUIDE_HREF}" -- "${paths[@]}" || true
      grep -RInE "${BT_GUIDE_PATH}" -- "${paths[@]}" || true
    } | grep -Ev "${STUB_ALLOW}" || true
  )"
  ec=$?
  set -e
  # Pipeline above always ends with grep -Ev || true → ec 0; keep guard for future edits.
  if [[ "${ec}" -gt 1 ]]; then
    echo "ERROR: grep failed while scanning for missing upstream doc links (exit ${ec})" >&2
    exit "${ec}"
  fi
  if [[ -n "${hits}" ]]; then
    echo "ERROR: product docs link to non-vendored docs/guides/* or docs/diagrams/* files." >&2
    printf '%s\n' "${hits}" >&2
    echo "Point at the pinned Shopanda commit on GitHub, or add a stub README only." >&2
    exit 1
  fi
}

scan_previous_pin_full_sha() {
  local hits ec
  set +e
  hits="$(grep -RInF "${PREVIOUS_PIN_FULL_SHA}" -- "${paths[@]}" || true)"
  ec=$?
  set -e
  if [[ "${ec}" -gt 1 ]]; then
    echo "ERROR: grep failed while scanning for previous pin SHA (exit ${ec})" >&2
    exit "${ec}"
  fi
  if [[ -n "${hits}" ]]; then
    echo "ERROR: product docs still contain previous pin full SHA ${PREVIOUS_PIN_FULL_SHA}." >&2
    printf '%s\n' "${hits}" >&2
    echo "Rewrite to the current pin (docs/UPSTREAM_PIN.md) or use an abbreviated historical form (e.g. 56818946…)." >&2
    exit 1
  fi
}

scan() {
  local pattern="$1"
  local label="$2"
  local hits ec
  set +e
  hits="$(grep -RInE "${pattern}" -- "${paths[@]}")"
  ec=$?
  set -e
  if [[ "${ec}" -gt 1 ]]; then
    echo "ERROR: grep failed while scanning product docs (exit ${ec})" >&2
    exit "${ec}"
  fi
  if [[ -n "${hits}" ]]; then
    echo "ERROR: product docs ${label}" >&2
    printf '%s\n' "${hits}" >&2
    echo "Name the path in prose or link the pinned upstream commit on GitHub." >&2
    exit 1
  fi
}

self_test
scan '\]\((\.\./)*(docs/)?phase-[0-9]' "contain relative links into docs/phase-* (those paths are not on this product tree)."
scan 'blob/dev/docs/phase-|tree/dev/docs/phase-' "contain hardcoded GitHub blob/tree URLs into docs/phase-* on dev."
scan_missing_upstream_docs
scan_previous_pin_full_sha
