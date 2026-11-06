# Runbook: local development environment

- **Introduced by slice:** `F0`, `F2`
- **Last exercised:** 2026-11-06 — `go build`, `ImportBoundary`, docs-policy + govuln baseline presence, `./app help` with the `.env` edits below; Metal Dogs DSN harness (`OpenIsolated` + `TestMetaStore_RoundTrip`) against compose Postgres on a published host port. Full `./app setup` / `./app dev` still need Postgres 17.
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
# GET /api/customizer/enabled → 200 when enabled; 404 when disabled
# (not a k8s probe — use /healthz and /readyz)
```

### Why stock `.env.example` alone fails

| Check | Behaviour |
| --- | --- |
| Empty `SHOPANDA_AUTH_JWT_SECRET` | Always rejected |
| Example DB password / `sslmode=disable` | Rejected unless `SHOPANDA_DEV_MODE=true` |

## Running tests

```bash
go test ./plugins/ -run ImportBoundary -count=1
go test ./plugins/metaldogs/... -count=1   # unit; DSN tests skip without SHOPANDA_TEST_DSN
bash .github/scripts/check-product-docs-links.sh
# govuln (needs pinned govulncheck on PATH, as in CI):
# bash .github/scripts/run-govulncheck.sh
```

### DSN-gated feature tests (F2)

Compose publishes Postgres on `127.0.0.1:${SHOPANDA_DATABASE_HOST_PORT:-5432}`. Default password is `changeme` (same as compose service defaults).

```bash
docker compose up -d postgres
# If host 5432 is already taken:
#   SHOPANDA_DATABASE_HOST_PORT=5433 docker compose up -d postgres
export SHOPANDA_TEST_DSN='postgres://shopanda:changeme@127.0.0.1:5432/shopanda?sslmode=disable'
# Adjust host port in the DSN when using SHOPANDA_DATABASE_HOST_PORT.

go test ./plugins/metaldogs/... -count=1
# F2 harness canaries only (MetaStore + OpenIsolated pool); needs jq:
bash .github/scripts/run-metaldogs-integration-tests.sh
# Shopanda DSN packages (postgres/migrate/b2b) — separate from F2 proof:
bash .github/scripts/run-integration-tests.sh

# Coverage profiles (CI: no-DSN packages vs integration-metaldogs for adapter SQL):
go test ./plugins/metaldogs/ ./plugins/metaldogs/httpapi/ ./plugins/metaldogs/ports/ ./plugins/metaldogs/testutil/ \
  -count=1 -covermode=atomic -coverprofile=coverage-metaldogs-nodsn.out
go tool cover -func=coverage-metaldogs-nodsn.out
COVERPROFILE=coverage-metaldogs-integration.out bash .github/scripts/run-metaldogs-integration-tests.sh
```

`plugins/metaldogs/testutil.OpenIsolated` creates a **unique** schema per call and returns a DB whose pool connections all start with that `search_path` (`pgx` RuntimeParams; URL or libpq keyword DSN). Any `search_path` already in `SHOPANDA_TEST_DSN` is overwritten. It runs `plugin:metaldogs:*` migrations and drops the schema on cleanup. Deterministic helpers: `FakeClock`, `NewSeqGenerator`. Product clock/ID ports live in `plugins/metaldogs/ports`. When Actions billing returns, require `CI / integration-metaldogs` on `main` branch protection before closing CR-03.

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
