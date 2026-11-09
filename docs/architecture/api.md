# Customizer HTTP API

Public surface under `/api/customizer/*` (plugin flag `plugins.metaldogs.enabled`). Guest responses use platform `Cache-Control: no-store` (F1 prefix list). Not k8s probes — use `/healthz` / `/readyz`.

## Enablement & health

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| `GET` | `/api/customizer/enabled` | public | `{"status":"ok","plugin":"metaldogs"}` |
| `GET` | `/api/customizer/health` | public | BoundQueue/Reader only; `503` + generic `degraded` when not ready |
| `GET` | `/api/v1/admin/customizer/health` | `jobs.read` | Per-type job depths |

## Templates (C1)

Published catalog only. Drafts and retired versions never appear. Responses omit prompts, model parameters, content hashes, and reference/thumbnail file paths.

| Method | Path | Auth | Success |
| --- | --- | --- | --- |
| `GET` | `/api/customizer/templates?genre=` | public | `200` `{"templates":[…]}` |
| `GET` | `/api/customizer/templates/{id}` | public | `200` single DTO |

### Query

- `genre` — optional exact genre filter. Omitted/empty → all published. Unknown genre → `200` with empty `templates` array.

### Path

- `{id}` — template UUID. Non-UUID or missing/draft/retired → **`404`** `{"error":"not_found"}` (not 403).

### Public DTO

```json
{
  "id": "uuid",
  "slug": "head-front-blackmetal",
  "genre": "black_metal",
  "style_category": "head_portrait",
  "version": 1,
  "published_at": "2026-11-09T12:00:00Z",
  "photo_requirements": { "pose": "head_front", "background_hint": "", "lighting_hint": "" },
  "text_fields": {
    "editable": [{ "key": "dog_name", "max_length": 12, "allowed_charset": "latin_polish" }],
    "fixed": []
  },
  "output_config": {
    "canvas_width": 1024,
    "canvas_height": 1024,
    "monochrome": true,
    "print_area": { "x": 0, "y": 0, "width": 1024, "height": 1024 }
  },
  "thumbnail_url": null
}
```

`thumbnail_url` stays `null` until asset ingest maps `thumbnail_asset_id` to a signed GET (C2+). Never derived from authoring `thumbnail_path`.

### Errors

| HTTP | Body | When |
| --- | --- | --- |
| `404` | `{"error":"not_found"}` | Unknown / non-UUID / not published |
| `503` | `{"error":"unavailable"}` | Customizer HTTP up but `Bootstrap.DB` missing |
| `500` | `{"error":"internal_error"}` | Store failure |

### Cache

Handlers do not set public `max-age`. Platform middleware forces `Cache-Control: no-store` on `/api/customizer/*`.

## Safe delivery

No published templates ship with C1; list returns `[]` until operators `metaldogs:templates publish`. Flag default off.
