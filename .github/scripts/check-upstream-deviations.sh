#!/usr/bin/env bash
# Paths that differ from the pinned SHA must be listed here, after the
# docs/PRODUCT_OWNED.md manifest exempts owned and companion rows, and hook
# rows whose expect holds and whose file still matches the pin. For
# .gitignore only the lines outside the marker block are compared to the pin.
# Unlisted diffs fail. Keep the list in sync with docs/architecture/overview.md.
# Ignored trees (product content, not this list): plugins/metaldogs, docs,
# .github/workflows, .scratch. Other markdown is not ignored.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

PIN="$(grep -E '^\| Pinned commit \|' docs/UPSTREAM_PIN.md | grep -oE '[0-9a-f]{40}' | head -n1 || true)"
if [[ ! "${PIN}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "ERROR: could not parse 40-char pin from docs/UPSTREAM_PIN.md" >&2
  exit 1
fi

# Diffs that are not manifest-exempt. '#' comments are not part of the path.
# Reasons match docs/architecture/overview.md.
ALLOWLIST=(
  .cursor/rules/pr-documentation-checklist.mdc # main-only checklist; .cursor is outside the manifest
  .gitattributes # merge=ours file; manifest describes the driver and does not name this path
  .github/scripts/check-product-docs-links.sh # hardened product-doc links; .github/scripts is outside the manifest
  .github/scripts/check-upstream-deviations.sh # this list
  .github/scripts/run-metaldogs-integration-tests.sh # Metal Dogs DSN suite; .github/scripts is outside the manifest
  ci-samples/README.md # relative guide/runbook links rewritten to the pin blob
  cmd/api/register_plugins_metaldogs_test.go # registration tests; manifest owns register_products.go only
  plugins/b2b/README.md # relative guide link rewritten to the pin blob
  plugins/example/README.md # relative guide link rewritten to the pin blob
  plugins/pimdemo/README.md # relative guide link rewritten to the pin blob
  plugins/sitekit/README.md # relative guide link rewritten to the pin blob
  plugins/slotsdemo/README.md # relative guide link rewritten to the pin blob
  plugins/warehousedemo/README.md # relative guide link rewritten to the pin blob
  scripts/product-init/seed/ci-samples/README.md # seed copy of ci-samples/README.md
  storefront/.gitkeep # empty storefront dir; storefront is outside the manifest
  storefront/README.md # product storefront narrative; storefront is outside the manifest
)

mapfile -t dirty < <(
  git diff --name-only "${PIN}" HEAD -- . \
    ':!plugins/metaldogs' \
    ':!docs' \
    ':!.github/workflows' \
    ':!.scratch' \
    | sort -u
)

dirty_file="$(mktemp)"
remain_file="$(mktemp)"
trap 'rm -f "${dirty_file}" "${remain_file}"' EXIT
if [[ "${#dirty[@]}" -gt 0 ]]; then
  printf '%s\n' "${dirty[@]}" >"${dirty_file}"
else
  : >"${dirty_file}"
fi

python3 - "${ROOT}" "${ROOT}/docs/PRODUCT_OWNED.md" "${dirty_file}" "${PIN}" >"${remain_file}" <<'PY'
import fnmatch
import pathlib
import subprocess
import sys

root = pathlib.Path(sys.argv[1])
manifest = pathlib.Path(sys.argv[2])
dirty_paths = pathlib.Path(sys.argv[3]).read_text(encoding="utf-8").splitlines()
pin = sys.argv[4]
text = manifest.read_text(encoding="utf-8").splitlines()

in_section = False
in_fence = False
rows = []
for line in text:
    if not in_section:
        if "Manifest (machine-readable)" in line:
            in_section = True
        continue
    if not in_fence:
        if line.strip().startswith("```"):
            in_fence = True
        continue
    if line.strip().startswith("```"):
        break
    raw = line.rstrip("\r")
    if not raw or raw.lstrip().startswith("#"):
        continue
    if "\t" not in raw:
        raise SystemExit(f"ERROR: PRODUCT_OWNED row must be tab-separated (got {raw!r})")
    parts = raw.split("\t")
    if len(parts) < 4:
        raise SystemExit(f"ERROR: PRODUCT_OWNED row needs path, kind, expect, note (got {raw!r})")
    rows.append((parts[0], parts[1], parts[2]))

if not any(k == "owned" for _, k, _ in rows) or not any(k == "hook" for _, k, _ in rows):
    raise SystemExit("ERROR: PRODUCT_OWNED manifest has no owned or hook rows")

def matches(path: str, row_path: str) -> bool:
    if row_path.endswith("/**"):
        prefix = row_path[:-3]
        return path == prefix or path.startswith(prefix + "/")
    if row_path.endswith("/"):
        return path.startswith(row_path)
    if "*" in row_path:
        return fnmatch.fnmatch(path, row_path)
    return path == row_path

MARKER_START = "# >>> shopanda-product-gitignore"
MARKER_END = "# <<< shopanda-product-gitignore"

def outside_marker_block(text: str):
    lines = text.splitlines()
    starts = [i for i, ln in enumerate(lines) if ln == MARKER_START]
    ends = [i for i, ln in enumerate(lines) if ln == MARKER_END]
    if not starts and not ends:
        return lines
    if len(starts) != 1 or len(ends) != 1 or starts[0] >= ends[0]:
        return None
    return lines[: starts[0]] + lines[ends[0] + 1 :]

def pin_text(rel: str):
    r = subprocess.run(
        ["git", "show", f"{pin}:{rel}"],
        cwd=root,
        capture_output=True,
    )
    if r.returncode != 0:
        return None
    return r.stdout.decode("utf-8")

def matches_pin_outside_expect(rel: str, expect: str) -> bool:
    if expect == "gitignore_markers":
        current = (root / rel).read_text(encoding="utf-8")
        pinned = pin_text(rel)
        if pinned is None:
            return False
        return outside_marker_block(current) == outside_marker_block(pinned)
    r = subprocess.run(
        ["git", "diff", "--quiet", pin, "HEAD", "--", rel],
        cwd=root,
    )
    if r.returncode == 0:
        return True
    if r.returncode == 1:
        return False
    raise SystemExit(f"ERROR: git diff --quiet failed for {rel} (exit {r.returncode})")

def hook_ok(rel: str, expect: str) -> bool:
    p = root / rel
    if expect == "exists":
        holds = p.is_file()
    elif expect == "gitignore_markers":
        if not p.is_file():
            return False
        lines = p.read_text(encoding="utf-8").splitlines()
        starts = [i for i, ln in enumerate(lines) if ln == MARKER_START]
        ends = [i for i, ln in enumerate(lines) if ln == MARKER_END]
        if len(starts) != 1 or len(ends) != 1 or starts[0] >= ends[0]:
            return False
        frag = root / ".gitignore.product"
        frag_lines = (
            [ln.rstrip("\r") for ln in frag.read_text(encoding="utf-8").splitlines()]
            if frag.is_file()
            else []
        )
        between = [ln.rstrip("\r") for ln in lines[starts[0] + 1 : ends[0]]]
        holds = between == frag_lines
    elif not p.is_file():
        return False
    elif expect.startswith("contains:"):
        holds = expect[len("contains:") :] in p.read_text(encoding="utf-8")
    elif expect.startswith("contains_all:"):
        needles = expect[len("contains_all:") :].split("|")
        body = p.read_text(encoding="utf-8")
        holds = all(n and n in body for n in needles)
    else:
        raise SystemExit(f"ERROR: unknown hook expect {expect!r} for {rel}")
    return holds and matches_pin_outside_expect(rel, expect)

def exempt(path: str) -> bool:
    for row_path, kind, expect in rows:
        if not matches(path, row_path):
            continue
        if kind in ("owned", "companion"):
            return True
        if kind == "hook" and hook_ok(row_path, expect):
            return True
    return False

for path in dirty_paths:
    path = path.strip()
    if path and not exempt(path):
        print(path)
PY

mapfile -t remaining <"${remain_file}"

unlisted=()
if [[ "${#remaining[@]}" -gt 0 ]]; then
  for path in "${remaining[@]}"; do
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
fi

stale=()
for a in "${ALLOWLIST[@]}"; do
  found=0
  if [[ "${#remaining[@]}" -gt 0 ]]; then
    for path in "${remaining[@]}"; do
      if [[ "${path}" == "${a}" ]]; then
        found=1
        break
      fi
    done
  fi
  if [[ "${found}" -eq 0 ]]; then
    stale+=("${a}")
  fi
done

if [[ "${#unlisted[@]}" -gt 0 || "${#stale[@]}" -gt 0 ]]; then
  if [[ "${#unlisted[@]}" -gt 0 ]]; then
    echo "ERROR: paths differ from pin ${PIN} but are not listed:" >&2
    printf '  %s\n' "${unlisted[@]}" >&2
  fi
  if [[ "${#stale[@]}" -gt 0 ]]; then
    echo "ERROR: listed paths do not differ from pin ${PIN} outside the manifest:" >&2
    printf '  %s\n' "${stale[@]}" >&2
  fi
  echo "Update docs/architecture/overview.md and this script together." >&2
  exit 1
fi

echo "upstream-deviations OK (pin ${PIN:0:12}, ${#ALLOWLIST[@]} listed path(s); manifest exempts owned/companion/hooks that match the pin)"
