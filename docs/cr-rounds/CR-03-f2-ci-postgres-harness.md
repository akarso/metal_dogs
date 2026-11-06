# CR-03: F2 CI & PostgreSQL test harness

- Closed: 2026-11-06
- Slices: `F2`
- Milestone progress: M0 → 3/6 slices (`F0`, `F1`, `F2` done)

## What shipped

Metal Dogs PostgreSQL test harness under `plugins/metaldogs/testutil`: `OpenIsolated` (per-package schema + plugin migrations), `FakeClock`, `SeqGenerator`. Trivial `MetaStore` port/adapter round-trip proves the harness. CI `run-integration-tests.sh` includes `./plugins/metaldogs/...` and canary `TestMetaStore_RoundTrip`. Unit + integration jobs emit Metal Dogs / suite coverage profiles as artifacts. Compose publishes Postgres on `127.0.0.1:${SHOPANDA_DATABASE_HOST_PORT:-5432}` for host-side `go test`.

## Verification results

| Item | Result |
| --- | --- |
| `OpenIsolated` without DSN | Skips cleanly |
| `OpenIsolated` with DSN | Creates schema, applies `plugin:metaldogs:*`, drops on cleanup |
| `TestMetaStore_RoundTrip` | Pass locally with compose Postgres |
| Integration canary wired | `adapters/postgres\|TestMetaStore_RoundTrip` |
| Coverage artifacts | Unit `coverage-metaldogs-unit`; integration `coverage-integration` (+ metaldogs grep) |
| ImportBoundary | Passes |
| FakeClock / SeqGenerator | Unit-tested in `testutil` |

## Tests added

- `plugins/metaldogs/testutil/clock_test.go`
- `plugins/metaldogs/adapters/postgres/meta_store_test.go` (`TestMetaStore_RoundTrip`)
- CI package list + canary in `.github/scripts/run-integration-tests.sh`

## Flags / migrations / rollback

- No new flags. No new production migrations (reuses F1 `metaldogs_meta`).
- Rollback: revert harness/CI-only; no runtime behaviour change.

## Documentation updated

- [x] ROADMAP F2 DONE; AGENTS.md + `runbooks/local-development.md` test/DSN/coverage commands
- [x] `plugins/metaldogs/README.md` harness note; compose host port publish
- [x] This CR record + log link

## Follow-ups filed

- `F3`: observability baseline (`correlation_id`, metrics, queue depth)
- GitHub Actions billing lock still blocks live CI green on `origin` — resolve account billing
- Pre-existing Shopanda DSN tests (`StoreCredit` uuid cast, notification dedupe reclaim) fail locally on this pin against fresh Postgres 17; not introduced by F2 — track on next upstream upgrade if still red in Actions
