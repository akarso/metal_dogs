# Slots Demo Reference Plugin

Reference implementation for storefront **UI slot** registration using the Stable v0 `pkg/extapi` surface.

## What it demonstrates

- Register renderers on `layout.footer` and `pdp.info` anchors
- Use `extapi.PlacementAppend` for additive HTML injection
- Emit identifiable `data-slotsdemo` markers for integration tests

## Enable

```yaml
plugins:
  slotsdemo:
    enabled: true
```

Or via environment:

```env
SHOPANDA_PLUGINS_SLOTSDEMO_ENABLED=true
```

Restart after changing config. Registration is wired in `cmd/api/register_plugins.go` when enabled.

## Authoring notes

- Use `app.Slots("<vendor>/<name>").RegisterRenderer(...)` with `extapi` anchor constants
- Theme templates must declare matching `{{slot_container "anchor"}}` or `{{slot . "anchor" "placement"}}` markers
- See [PLUGINS.md](../../PLUGINS.md) and [EXTENSION_API.md](https://github.com/akarso/shopanda/blob/a8cbcb18c5054178ba5e5f0e4980f82b0a7ed512/docs/guides/EXTENSION_API.md)
