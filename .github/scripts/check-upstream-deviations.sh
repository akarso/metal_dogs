#!/usr/bin/env bash
# Absorb canary: platform paths that differ from the pinned upstream SHA must be
# allowlisted here (keep in sync with docs/architecture/overview.md).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

PIN="$(grep -E '^\| Pinned commit \|' docs/UPSTREAM_PIN.md | grep -oE '[0-9a-f]{40}' | head -n1 || true)"
if [[ ! "${PIN}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "ERROR: could not parse 40-char pin from docs/UPSTREAM_PIN.md" >&2
  exit 1
fi

# Paths under these roots may differ from the pin only if listed below.
ALLOWLIST=(
  cmd/api/bind_queue_test.go
  cmd/api/cache_control_prefixes.go
  cmd/api/cache_control_prefixes_test.go
  cmd/api/job_queue_bind.go
  cmd/api/jobs_cli.go
  cmd/api/main.go
  cmd/api/migrate_plugins_test.go
  cmd/api/register_plugins.go
  cmd/api/register_plugins_metaldogs_test.go
  cmd/api/wire_routes.go
  internal/application/integration/sync_manual_roundtrip_test.go
  internal/application/integration/sync_runtime.go
  internal/application/integration/sync_runtime_test.go
  internal/domain/jobs/admin.go
  internal/domain/jobs/job.go
  internal/domain/jobs/worker.go
  internal/domain/jobs/worker_test.go
  internal/platform/config/config.go
  internal/platform/config/config_test.go
  internal/platform/plugin/providers.go
  internal/platform/plugin/providers_test.go
  internal/platform/plugin/sync_job_test.go
  internal/platform/runtime/scheduler_test.go
)

mapfile -t dirty < <(
  git diff --name-only "${PIN}" HEAD -- \
    internal/platform \
    internal/application/integration \
    internal/domain/jobs \
    cmd/api \
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

if [[ "${#unlisted[@]}" -gt 0 ]]; then
  echo "ERROR: platform paths differ from pin ${PIN} but are not on the absorb allowlist:" >&2
  printf '  %s\n' "${unlisted[@]}" >&2
  echo "Update docs/architecture/overview.md and this script together." >&2
  exit 1
fi

echo "upstream-deviations OK (pin ${PIN:0:12}, ${#dirty[@]} allowlisted path(s))"
