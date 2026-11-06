# CR-03: F2 CI & PostgreSQL test harness

- Closed: — (merged; **origin CI proof still open** — billing lock only)
- Slices: `F2`
- Milestone progress: M0 → 3/6 slices merged (`F0`, `F1`, `F2`); F2 process-close blocked on `CI / integration-metaldogs` green on origin (billing)

## What shipped

Metal Dogs PostgreSQL test harness under `plugins/metaldogs/testutil`: `OpenIsolated` creates a **unique** schema per call and returns a `*sql.DB` with `search_path` pinned on **every** pool connection via `pgx.ParseConfig` + `RuntimeParams` (URL and libpq keyword DSNs). Applies `plugin:metaldogs:*` migrations; drops schema on cleanup via admin DSN (`t.Logf` on DROP failure). Deterministic helpers: `FakeClock`, `SeqGenerator` / `FixedGenerator`. Product `Clock` / `IDGenerator` in `ports/`. `MigrationFS()` / `MigrationDir` expose embed FS for the harness.

Dedicated CI job `integration-metaldogs` runs `.github/scripts/run-metaldogs-integration-tests.sh` (canaries: `TestMetaStore_RoundTrip`, `TestOpenIsolated_PoolSeesSchema`). Shopanda DSN suite remains in `integration` / `run-integration-tests.sh` and cannot fail F2 proof.

## Verification results

| Item | Result |
| --- | --- |
| Pool `search_path` | `current_schema()` equality across 20 goroutines; final row count == workers |
| Same slug / cleanup | Distinct schemas; schema absent from `pg_namespace` after cleanup |
| URL + keyword DSN parse | Unit tests on `configWithSearchPath` |
| Origin `integration-metaldogs` | **Blocked** — Actions billing (open gate; billing-only) |

## Open gate

1. Restore GitHub Actions billing.
2. Confirm **branch protection** on `main` requires the check named exactly `CI / integration-metaldogs` (workflow comment documents this; a missing or stale required-check name will not enforce the decoupled job).
3. Confirm that check is **green** on `origin/main`.
4. Only then mark F2 process-closed / this CR `Closed:`.

Shopanda `StoreCredit` / notification-dedupe reds do not block `integration-metaldogs`.

## CR-03 follow-up fixes

- Pooled isolation via `RuntimeParams` (not one-shot `SET` / URL-only mangling)
- Decoupled metaldogs integration job
- ROADMAP goal: unique schema per `OpenIsolated` call
- Stronger pool assertions; DROP failures logged under `-v`
