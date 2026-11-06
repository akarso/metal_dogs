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
├── migrations.go      # plugin.Migrator + RunMigrationsForTest
├── migrations/        # SQL (namespaced plugin:metaldogs:*)
├── httpapi/           # public HTTP adapters
├── domain/            # domain model (empty until C*)
├── application/       # use cases (empty until C*)
├── ports/             # Clock, IDGenerator, MetaStore (+ later C*/G*)
├── adapters/postgres/ # MetaStore adapter (F2 harness exercise)
└── testutil/          # OpenIsolated, FakeClock, NewSeqGenerator (fakes only)
```


## Current surface (F1)

| Method | Path | When enabled |
| --- | --- | --- |
| `GET` | `/api/customizer/enabled` | `{"status":"ok","plugin":"metaldogs"}` |

**Not a liveness/readiness probe.** Use `/healthz` and `/readyz` for orchestrators. This route sits on the full API middleware stack (including `StoreMiddleware`) and can hang if the database is down. Guest responses for `/api/customizer/*` are forced `Cache-Control: no-store`.

## Test harness (F2)

```bash
export SHOPANDA_TEST_DSN='postgres://shopanda:changeme@127.0.0.1:5432/shopanda?sslmode=disable'
go test ./plugins/metaldogs/... -count=1
```

`testutil.OpenIsolated(t, slug)` creates a unique schema per call, forces `search_path` on every pool connection (`pgx` RuntimeParams; URL or keyword DSN — any pre-set `search_path` in `SHOPANDA_TEST_DSN` is overwritten), applies plugin migrations, and cleans up. CI job `integration-metaldogs` canaries: `TestMetaStore_RoundTrip`, `TestOpenIsolated_PoolSeesSchema`.

Job types for later slices: `integration.sync.metaldogs.<name>` (see ADR-0003).
