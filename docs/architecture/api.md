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

Multipart field name: **`file`**. Rows are unauthenticated until C3; schema has nullable `owner_ref` for capability/session bind. Storage keys are never returned. Plugin flag default off.

**Concurrency (pre-O4):** in-process semaphore `DefaultMaxConcurrentUploads` (4) per API process — **not** per IP and **not** cluster-wide. Ops ceiling ≈ `replicas × 4` until O4 adds route/IP limits. Do not treat this as abuse protection.

### Limits

| Constraint | Value |
| --- | --- |
| Accepted types | JPEG, PNG only (magic sniff; extension ignored). WebP → `415` until re-encode path exists |
| Max payload | `generation.MaxPhotoBytes` (20 MiB); streamed via multipart reader (no double form buffer) |
| Decoded bounds | `DecodeConfig` OOM gate (max edge / pixels; min deferred until upright) then upright min **256×256**, max **2048×2048** |
| Pipeline | sniff → DecodeConfig bounds → full decode → EXIF Orientation normalize → re-encode (strip metadata) → `sha256:<hex>` → Put + insert |
| Preview URL TTL | `ports.DefaultSignedGetTTL` (60s) — see `preview_url_expires_at` |
| Retention stub | `delete_after = now + 2h` on insert; sweep is **O2** (not C2). Put-then-crash before Insert can orphan blobs with no row until store/DB reconcile lands in O2. |
| Failure cleanup | Insert fail → Delete blob (errors surfaced). Sign fail → SoftDelete first, then Delete; SoftDelete failure leaves row+blob consistent and returns `500` |

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
| `429` | `{"error":"too_many_requests"}` + `Retry-After: 1` | In-process upload concurrency cap (per process) |
| `503` | `{"error":"unavailable"}` | No DB or AssetStore wired |
| `500` | `{"error":"internal_error"}` | Put/insert/sign failure (after compensating cleanup) |

## Designs (C3)

| Method | Path | Auth | Success |
| --- | --- | --- | --- |
| `POST` | `/api/customizer/designs` | public | `201` design DTO + `owner_token` once |
| `GET` | `/api/customizer/designs/{id}` | `Authorization: Bearer <owner_token>` | `200` design DTO |
| `PATCH` | `/api/customizer/designs/{id}` | Bearer | `200` design DTO |

Token in query string is rejected (`400`). Wrong/missing Bearer or unknown id → **`404` `not_found`** (never `403`). Token is never logged; hash never returned.

### Create body

```json
{ "template_id": "uuid", "upload_id": "uuid", "texts": { "dog_name": "Burek" } }
```

All three fields required. Template must be published. Upload must be an active unbound `source` asset. Editable texts validated (charset/max length); unknown keys rejected; fixed template texts merged server-side. Asset `owner_ref` set to design id.

### PATCH body

Optional `texts` and/or `upload_id`. Allowed only when status is `draft`, `generation_failed`, or `preview_ready` (else `409` `invalid_transition`). Replace-photo clears previous asset `owner_ref` and binds the new upload.

### Success DTO

```json
{
  "id": "uuid",
  "status": "draft",
  "template_id": "uuid",
  "template_version": 1,
  "texts": { "dog_name": "Burek", "tagline": "METAL" },
  "upload_id": "uuid",
  "created_at": "2026-11-10T12:00:00Z",
  "expires_at": "2026-12-10T12:00:00Z",
  "source_preview_url": "https://…signed…",
  "source_preview_url_expires_at": "2026-11-10T12:01:00Z",
  "owner_token": "base64url…"
}
```

`owner_token` appears only on create. `expires_at` is design retention (30d unordered). Source preview TTL is 60s.

### Errors

| HTTP | Body | When |
| --- | --- | --- |
| `400` | `bad_request` | Malformed JSON / token in query / empty patch |
| `404` | `not_found` | Unknown id / missing or wrong Bearer |
| `409` | `invalid_transition` | PATCH in non-editable status |
| `422` | `template_unavailable` | Template not published |
| `422` | `upload_unavailable` | Missing/deleted/non-source upload |
| `422` | `upload_already_attached` | Upload `owner_ref` already set |
| `422` | `invalid_texts` | Charset / length / unknown or missing keys |
| `503` | `unavailable` | No DB / AssetStore / catalog |
| `500` | `internal_error` | Persistence or sign failure |

## Safe delivery

No published templates ship with C1; list returns `[]` until operators `metaldogs:templates publish`. Uploads and designs require media + DB. Flag default off.
