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
├── plugin.go              # Init + Name + AssetStore wiring
├── cli.go                 # metaldogs:eval (G0)
├── assets.go              # wrap Bound/registered SignedURLStorage
├── sync_obs.go            # obs.ping RegisterManual
├── migrations.go          # plugin.Migrator + MigrationFS
├── migrations/            # SQL (namespaced plugin:metaldogs:*)
├── generation/            # ImageProvider port + fake (ADR-0008 / G0)
│   └── eval/              # offline runner + report.json
├── httpapi/               # enabled + health
├── observability/         # correlation_id alias helpers
├── domain/                # domain model (empty until C*)
├── application/           # use cases (empty until C*)
├── ports/                 # Clock, IDGenerator, MetaStore, AssetStore
├── adapters/postgres/     # MetaStore adapter (F2 harness exercise)
├── adapters/mediastore/   # wrap media.SignedURLStorage
└── testutil/              # OpenIsolated, FakeClock, NewSeqGenerator (fakes only)
```

## Current surface

| Method | Path | Auth | When enabled |
| --- | --- | --- | --- |
| `GET` | `/api/customizer/enabled` | public | `{"status":"ok","plugin":"metaldogs"}` |
| `GET` | `/api/customizer/health` | public | BoundQueue/Reader readiness only; generic `degraded` reason; HTTP 503 when degraded |
| `GET` | `/api/v1/admin/customizer/health` | `jobs.read` | Per-type depths via `BoundQueue` + `CountsByType` |

**Not k8s probes.** Use `/healthz` and `/readyz`. Guest `/api/customizer/*` responses are `Cache-Control: no-store`. Depths stay admin-only (backlog/failure recon).

Sync job (F3 sample): `integration.sync.metaldogs.obs.ping` — platform injects `request_id` on Enqueue; MD logs also emit `correlation_id` (same value). Platform contract: [shopanda#568](https://github.com/akarso/shopanda/pull/568) at pin `a8cbcb18`, plus Metal Dogs residuals `App.BoundQueue()` and sync-handler `request_id`→ctx reinjection (see [`docs/architecture/overview.md`](../../docs/architecture/overview.md) deviations).

## Private assets (F4 / ADR-0004)

- Port: `ports.AssetStore` (`Put` / `Head` / `SignedGetURL` / `Delete`). **No writers yet.**
- Keys: `private/blobs/metaldogs/...` via `ports.ObjectKey`; mediastore **rejects** any other key.
- Delivery: wrap core `media.SignedURLStorage` (inline signed GET; TTL capped ≤15m). No product shim.
- Local: `SHOPANDA_MEDIA_LOCAL_BASE_PATH=./var/media` (never FileServer); `SHOPANDA_MEDIA_LOCAL_SIGN_SECRET` (≥32 bytes) outside DEV.

## Test harness (F2)

```bash
export SHOPANDA_TEST_DSN='postgres://shopanda:changeme@127.0.0.1:5432/shopanda?sslmode=disable'
go test ./plugins/metaldogs/... -count=1
```

`testutil.OpenIsolated` — unique schema per call, forced `search_path`. CI: `integration-metaldogs` canaries.

Later job types: `integration.sync.metaldogs.<name>` (ADR-0003).

## Offline eval (G0)

With the plugin enabled (`SHOPANDA_PLUGINS_METALDOGS_ENABLED=true`). Media is optional for Init — without it `AssetStore` stays nil but `metaldogs:eval` still registers:

```bash
./app metaldogs:eval \
  -template plugins/metaldogs/generation/eval/testdata/template.json \
  -photos /path/to/photos \
  -out /tmp/metaldogs-eval \
  -provider fake
```

Writes `photo.ext.png` per input (e.g. `dog.jpg.png`) plus `report.json` (parameters/seed, latency / cost slot; validation `skipped` until G3). `--out` must be empty. Default provider is `fake` (deterministic). Canvas capped at 2048². Empty photo bytes are rejected. No HTTP route. Non-zero exit if any sample fails.
