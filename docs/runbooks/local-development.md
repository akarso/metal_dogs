# Runbook: local development environment

- **Introduced by slice:** `F0`, `F2`
- **Last exercised:** 2026-11-06 — `go build`, `ImportBoundary`, docs-policy + govuln baseline presence, `./app help` with the `.env` edits below (Postgres not required for `help`). Full `./app setup` / `./app dev` still need a running Postgres 17 (compose).
- **Severity when triggered:** low
- **Owner:** every developer

## Purpose

Bring up Shopanda with the (future) `metaldogs` plugin, PostgreSQL, local object storage, and the fake AI/POD providers; run the test suites.

## Prerequisites

- Go toolchain: **1.26.x** (matches root `go.mod`).
- PostgreSQL **17** (matches `docker-compose.yml` / CI image). Native Postgres is fine if you match that major.
- Object storage: local filesystem adapter by default; MinIO optional for S3 contract tests (`F4`).
- No AI or POD credentials are needed; providers default to `fake` once generation lands.

## Remotes and branches

```bash
git remote -v
# origin    → git@github.com:akarso/metal_dogs.git   (this product; push here)
# upstream  → https://github.com/akarso/shopanda.git (Shopanda; fetch upgrades only)
```

Metal Dogs is **main-only**. Do not create a Shopanda-style `dev` branch or run `scripts/promote-dev-to-main.sh` here (upstream tooling; unused).

Pin: [`docs/UPSTREAM_PIN.md`](../UPSTREAM_PIN.md).

## Bring-up (copy-paste)

```bash
cp .env.example .env

# JWT is always required (even when DEV_MODE is false).
# DEV_MODE=true is required for the example DB password + sslmode=disable.
# Generate a secret, then set both:
SECRET="$(openssl rand -hex 32)"
# Portable .env edits (GNU and BSD sed): rewrite via temp file, not `sed -i`.
set_env() { # set_env KEY VALUE
  local key="$1" val="$2" tmp
  tmp="$(mktemp)"
  if grep -q "^${key}=" .env; then
    awk -v k="${key}" -v v="${val}" 'BEGIN{FS=OFS="="} $1==k{$0=k"="v} {print}' .env >"${tmp}"
  else
    cat .env >"${tmp}"
    printf '%s=%s\n' "${key}" "${val}" >>"${tmp}"
  fi
  mv "${tmp}" .env
}
set_env SHOPANDA_AUTH_JWT_SECRET "${SECRET}"
set_env SHOPANDA_DEV_MODE true

# Postgres 17 (compose) — or point .env at a native instance
docker compose up -d postgres

go build -o app ./cmd/api
./app help                      # needs the .env edits above; DB optional
./app setup                     # migrate + seed (needs Postgres)
./app dev                       # HTTP + embedded worker + scheduler
```

Open the default Shopanda storefront on the configured HTTP port (see `.env.example`).

Enable the plugin (optional for local customizer work):

```bash
set_env SHOPANDA_PLUGINS_METALDOGS_ENABLED true
# or YAML: plugins.metaldogs.enabled: true
./app dev
# GET /api/customizer/health → 200 when enabled; 404 when disabled
```

### Why stock `.env.example` alone fails

| Check | Behaviour |
| --- | --- |
| Empty `SHOPANDA_AUTH_JWT_SECRET` | Always rejected |
| Example DB password / `sslmode=disable` | Rejected unless `SHOPANDA_DEV_MODE=true` |

## Running tests

```bash
go test ./plugins/ -run ImportBoundary -count=1
bash .github/scripts/check-product-docs-links.sh
# govuln (needs pinned govulncheck on PATH, as in CI):
# bash .github/scripts/run-govulncheck.sh
```

- Broader Go unit/feature harness: `F2`.
- Storefront unit and E2E: `S1`.
- Opt-in smoke against real providers: `METALDOGS_SMOKE=1` plus credentials in env; costs money; never in CI.

## Resetting state

- Drop and recreate the dev database, rerun `./app setup`.
- Clear the local asset directory once `F4` defines it.

## Known pitfalls

- Do not develop Metal Dogs by editing a separate local Shopanda clone and copying files — merge via `upstream` only.
- Metal Dogs owns product docs under `docs/`; upstream guides are linked at the pin on GitHub (`docs/guides/README.md`).
- Before the next upstream upgrade: ensure this clone is **not shallow** (`test ! -f .git/shallow` or `git fetch --unshallow upstream`).

## Prevention / follow-up

- `AGENTS.md` must list the same commands; keep both in sync at CR-round closure.
