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

- `genre` — optional **exact, case-sensitive** genre filter (must match authored `genre` byte-for-byte after trim). Omitted/empty → all published. Unknown / differently cased genre (e.g. `Black_Metal` vs `black_metal`) → `200` with empty `templates` array.

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

`thumbnail_url` stays `null` until asset ingest maps `thumbnail_asset_id` to a signed GET (C5 / later). Never derived from authoring `thumbnail_path`.

### Errors

| HTTP | Body | When |
| --- | --- | --- |
| `404` | `{"error":"not_found"}` | Unknown / non-UUID / not published |
| `503` | `{"error":"unavailable"}` | Customizer HTTP up but `Bootstrap.DB` missing |
| `500` | `{"error":"internal_error"}` | Store failure |

### Cache

Handlers do not set public `max-age`. Platform middleware forces `Cache-Control: no-store` on `/api/customizer/*`.

## Uploads (C2)

| Method | Path | Auth | Success |
| --- | --- | --- | --- |
| `POST` | `/api/customizer/uploads` | public | `201` upload DTO |

Multipart field name: **`file`**. Rows are unauthenticated until C3; schema has nullable `owner_ref` for capability/session bind. Storage keys are never returned. Plugin flag default off; in-process concurrency cap (`DefaultMaxConcurrentUploads`) until O4.

### Limits

| Constraint | Value |
| --- | --- |
| Accepted types | JPEG, PNG only (magic sniff; extension ignored). WebP → `415` until re-encode path exists |
| Max payload | `generation.MaxPhotoBytes` (20 MiB); streamed via multipart reader (no double form buffer) |
| Decoded bounds | `DecodeConfig` first (OOM gate) then upright min **256×256**, max **2048×2048** (both edges) + pixel cap |
| Pipeline | sniff → DecodeConfig bounds → full decode → EXIF Orientation normalize → re-encode (strip metadata) → `sha256:<hex>` → Put + insert |
| Preview URL TTL | `ports.DefaultSignedGetTTL` (60s) — see `preview_url_expires_at` |
| Retention stub | `delete_after = now + 2h` on insert; sweep is **O2** (not C2) |
| Failure cleanup | Insert or SignedGetURL failure after Put → best-effort blob Delete; sign failure also SoftDelete row |

### Success DTO (`201`)

```json
{
  "upload_id": "uuid",
  "preview_url": "https://…signed…",
  "preview_url_expires_at": "2026-11-09T12:01:00Z",
  "delete_after": "2026-11-09T14:00:00Z",
  "width": 1024,
  "height": 1024,
  "mime_type": "image/jpeg"
}
```

Do **not** treat `delete_after` as preview validity — use `preview_url_expires_at` for the signed URL.

### Errors

| HTTP | Body | When |
| --- | --- | --- |
| `400` | `{"error":"bad_request"}` | Missing `file` field / malformed multipart |
| `413` | `{"error":"payload_too_large"}` | Body or file exceeds 20 MiB |
| `415` | `{"error":"unsupported_media_type"}` | Non-JPEG/PNG (incl. WebP) or garbage with `.jpg` name |
| `422` | `{"error":"invalid_image"}` | Magic ok but decode fails |
| `422` | `{"error":"dimensions_out_of_range"}` | Outside 256–2048 on either edge (config or upright) |
| `429` | `{"error":"too_many_requests"}` | In-process upload concurrency cap |
| `503` | `{"error":"unavailable"}` | No DB or AssetStore wired |
| `500` | `{"error":"internal_error"}` | Put/insert/sign failure (after compensating cleanup) |

## Safe delivery

No published templates ship with C1; list returns `[]` until operators `metaldogs:templates publish`. Uploads require media + DB. Flag default off.
