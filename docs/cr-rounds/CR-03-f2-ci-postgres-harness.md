# CR-03: F2 CI & PostgreSQL test harness

- Closed: — (merged; **origin CI proof still open** — billing lock)
- Slices: `F2`
- Milestone progress: M0 → 3/6 slices merged (`F0`, `F1`, `F2`); F2 process-close blocked on origin canary green or explicit billing-only gate acceptance

## What shipped

Metal Dogs PostgreSQL test harness under `plugins/metaldogs/testutil`: `OpenIsolated` creates a **unique** schema per call and returns a `*sql.DB` with `search_path` pinned on **every** pool connection via libpq `options=-csearch_path=…` (not a one-shot `SET` on a pooled handle). Applies `plugin:metaldogs:*` migrations into that schema; drops schema on cleanup via a separate admin connection. Deterministic helpers: `FakeClock`, `SeqGenerator` / `FixedGenerator` (test fakes only). Product `Clock` / `IDGenerator` live in `ports/`. `MigrationFS()` exposes embed FS for the harness; no `RunMigrationsForTest` production API.

Trivial `MetaStore` port/adapter round-trip + pool isolation / cleanup / same-slug tests. CI `run-integration-tests.sh` includes `./plugins/metaldogs/...` and canaries `TestMetaStore_RoundTrip`, `TestOpenIsolated_PoolSeesSchema`. No-DSN unit coverage artifact excludes `adapters/postgres` (DSN SQL coverage is integration-only). Compose publishes Postgres on `127.0.0.1:${SHOPANDA_DATABASE_HOST_PORT:-5432}` for host-side `go test`.

## Verification results

| Item | Result |
| --- | --- |
| `OpenIsolated` without DSN | Skips cleanly |
| Pool `search_path` | `TestOpenIsolated_PoolSeesSchema` (MaxOpenConns=8, 20 goroutines) |
| Same slug twice | Distinct schemas; no cross-visibility |
| Cleanup | Schema absent from `pg_namespace` after inner test cleanup |
| `TestMetaStore_RoundTrip` / empty key | Pass locally with compose Postgres |
| Integration canaries wired | MetaStore + PoolSeesSchema |
| Coverage | no-DSN unit artifact; integration profile for adapter SQL |
| ImportBoundary | Passes |
| Origin Actions canaries | **Blocked** — account billing lock (open gate) |

## Tests added

- `plugins/metaldogs/testutil/db_test.go` (pool, same-slug, cleanup)
- `plugins/metaldogs/testutil/clock_test.go` (FakeClock Set/Advance, Seq, Fixed)
- `plugins/metaldogs/adapters/postgres/meta_store_test.go` (round-trip, empty key)
- CI package list + canaries in `.github/scripts/run-integration-tests.sh`

## Flags / migrations / rollback

- No new flags. No new production migrations (reuses F1 `metaldogs_meta`).
- Rollback: revert harness/CI-only; no runtime behaviour change.

## Documentation updated

- [x] ROADMAP F2 MERGED + open CI gate; AGENTS.md + `runbooks/local-development.md`
- [x] `plugins/metaldogs/README.md`; overview deviations; compose host port
- [x] This CR record + log link

## Follow-ups filed

- **Open gate:** Restore GitHub Actions billing and confirm integration canaries green on `origin` before calling F2 process-closed
- `F3`: observability baseline (`correlation_id`, metrics, queue depth)
- Pre-existing Shopanda DSN tests (`StoreCredit` uuid cast, notification dedupe reclaim) fail locally on this pin against fresh Postgres 17; not introduced by F2 — track on next upstream upgrade if still red in Actions

## CR-03-fix (review)

- Fixed pooled `search_path` isolation (DSN startup GUC)
- Unique schema per `OpenIsolated` call
- Isolation regression tests + canary
- Moved Clock/ID product types to `ports/`; removed `RunMigrationsForTest`
- Honest no-DSN vs integration coverage split; coverage upload only on integration success
