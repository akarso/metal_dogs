# plugins/metaldogs

Metal Dogs Shopanda external plugin.

## Enable

Default **off** (dark). Enable with:

```yaml
plugins:
  metaldogs:
    enabled: true
```

or `SHOPANDA_PLUGINS_METALDOGS_ENABLED=true`.

Registration: `cmd/api/register_plugins.go` (compile-time). Enabling also registers the plugin `Migrator` (`plugin:metaldogs:*` versions) for `app migrate` / boot.

## Layout

```text
plugins/metaldogs/
├── plugin.go          # Init + Name
├── sync_obs.go        # obs.ping RegisterManual
├── migrations.go      # plugin.Migrator + MigrationFS
├── migrations/        # SQL (namespaced plugin:metaldogs:*)
├── httpapi/           # enabled + health
├── observability/     # correlation_id alias helpers
├── domain/            # domain model (empty until C*)
├── application/       # use cases (empty until C*)
├── ports/             # Clock, IDGenerator, MetaStore (+ later C*/G*)
├── adapters/postgres/ # MetaStore adapter (F2 harness exercise)
└── testutil/          # OpenIsolated, FakeClock, NewSeqGenerator (fakes only)
```

## Current surface

| Method | Path | Auth | When enabled |
| --- | --- | --- | --- |
| `GET` | `/api/customizer/enabled` | public | `{"status":"ok","plugin":"metaldogs"}` |
| `GET` | `/api/customizer/health` | public | BoundQueue/Reader readiness only; generic `degraded` reason; HTTP 503 when degraded |
| `GET` | `/api/v1/admin/customizer/health` | `jobs.read` | Per-type depths via `BoundQueue` + `CountsByType` |

**Not k8s probes.** Use `/healthz` and `/readyz`. Guest `/api/customizer/*` responses are `Cache-Control: no-store`. Depths stay admin-only (backlog/failure recon).

Sync job (F3 sample): `integration.sync.metaldogs.obs.ping` — platform injects `request_id` on Enqueue; MD logs also emit `correlation_id` (same value). Platform contract: [shopanda#568](https://github.com/akarso/shopanda/pull/568) (guide merges with that PR; pin absorb still open).

## Test harness (F2)

```bash
export SHOPANDA_TEST_DSN='postgres://shopanda:changeme@127.0.0.1:5432/shopanda?sslmode=disable'
go test ./plugins/metaldogs/... -count=1
```

`testutil.OpenIsolated` — unique schema per call, forced `search_path`. CI: `integration-metaldogs` canaries.

Later job types: `integration.sync.metaldogs.<name>` (ADR-0003).
