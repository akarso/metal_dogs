# Runbook: local development environment

- **Introduced by slice:** `F0`, `F2`
- **Last exercised:** 2026-11-05 (`F0` build + ImportBoundary only)
- **Severity when triggered:** low
- **Owner:** every developer

## Purpose

Bring up Shopanda with the (future) `metaldogs` plugin, PostgreSQL, local object storage, and the fake AI/POD providers; run the test suites.

## Prerequisites

- Go toolchain version: **1.26.0** (matches root `go.mod` at the upstream pin).
- PostgreSQL: use the version from upstream `.env.example` / Docker Compose at the pin (typically current stable PostgreSQL 16+).
- Object storage: local filesystem adapter by default; MinIO optional for S3 contract tests (`F4`).
- No AI or POD credentials are needed; providers default to `fake` once generation lands.

## Remotes

```bash
git remote -v
# origin    → github.com/akarso/metal_dogs   (this product)
# upstream  → github.com/akarso/shopanda     (Shopanda; fetch upgrades only)
```

Pin: [`docs/UPSTREAM_PIN.md`](../UPSTREAM_PIN.md).

## Bring-up

```bash
cp .env.example .env
# Required for local CLI/boot with the stock example values:
#   SHOPANDA_DEV_MODE=true
#   SHOPANDA_AUTH_JWT_SECRET=$(openssl rand -hex 32)
# PostgreSQL must be reachable (docker compose up -d postgres, or native).
go build -o app ./cmd/api
./app setup                     # connectivity check, migrations, seed
./app dev                       # HTTP + embedded worker + scheduler
./app help                      # live command list (needs a valid local .env)
```

Open the default Shopanda storefront on the configured HTTP port (see `.env.example`). `docker compose up -d` is the upstream-supported path for Postgres + app processes.

Plugin enablement (`plugins.metaldogs.enabled`) lands in `F1`. Until then `plugins/metaldogs` is an empty placeholder and is not registered.

## Running tests

```bash
go test ./plugins/ -run ImportBoundary -count=1
```

- Broader Go unit/feature harness: `F2`.
- Storefront unit and E2E: `S1`.
- Opt-in smoke against real providers: `METALDOGS_SMOKE=1` plus credentials in env; costs money; never in CI.

## Resetting state

- Drop and recreate the dev database, rerun `./app setup`.
- Clear the local asset directory once `F4` defines it.

## Known pitfalls

- Do not develop Metal Dogs by editing a separate local Shopanda clone and copying files — merge via `upstream` only.
- Metal Dogs owns `docs/`; upstream documentation for the pin lives on GitHub, not in this tree.

## Prevention / follow-up

- `AGENTS.md` must list the same commands; keep both in sync at CR-round closure.
