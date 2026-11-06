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
├── migrations.go      # plugin.Migrator
├── migrations/        # SQL (namespaced plugin:metaldogs:*)
├── httpapi/           # public HTTP adapters
├── domain/            # domain model (empty until C*)
├── application/       # use cases (empty until C*)
├── ports/             # outbound ports (empty until C*/G*)
└── adapters/          # postgres / storage / provider adapters
```

## Current surface (F1)

| Method | Path | When enabled |
| --- | --- | --- |
| `GET` | `/api/customizer/enabled` | `{"status":"ok","plugin":"metaldogs"}` |

**Not a liveness/readiness probe.** Use `/healthz` and `/readyz` for orchestrators. This route sits on the full API middleware stack (including `StoreMiddleware`) and can hang if the database is down. Guest responses for `/api/customizer/*` are forced `Cache-Control: no-store`.

Job types for later slices: `integration.sync.metaldogs.<name>` (see ADR-0003).
