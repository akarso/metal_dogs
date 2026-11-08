# plugins/sitekit

Reference output of `scripts/product-init.sh --slug sitekit` (Phase 17).

Unregistered by default — copy the registration snippet into
`cmd/api/register_plugins.go` when you want to exercise it, or re-run
product-init for your own slug.

## What it demonstrates

- `GET /api/sitekit/health` via `RegisterPublicRoute`
- `plugin.Migrator` with `migrations/001_create_meta.sql`
- Stable name `sitekit` (migration namespace `plugin:sitekit:…`)

## Jobs

Canonical types only: `integration.sync.sitekit.<name>` via `pluginsdk`
`RegisterManual` / `Enqueue`. See
[PLUGIN_RUNTIME_CONTRACT](https://github.com/akarso/shopanda/blob/a8cbcb18c5054178ba5e5f0e4980f82b0a7ed512/docs/guides/PLUGIN_RUNTIME_CONTRACT.md).

## Checks

```bash
go test ./plugins/sitekit/ -count=1
make check
```
