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

Registration: `cmd/api/register_plugins.go` (compile-time).

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
| `GET` | `/api/customizer/health` | `{"status":"ok","plugin":"metaldogs"}` |

Job types for later slices use Shopanda sync jobs: `integration.sync.metaldogs.<name>` (see ADR-0003).
