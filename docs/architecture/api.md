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

Platform `AuthMiddleware` does **not** JWT-parse `Authorization` on `/api/customizer` and `/api/customizer/*` (owner tokens are opaque base64url, not Shopanda JWTs; scheme match is case-insensitive). Token in query string is rejected (`400`). Wrong/missing Bearer, unknown id, or past `expires_at` → **`404` `not_found`** (never `403`). Token is never logged; hash never returned.

### Create body

```json
{ "template_id": "uuid", "upload_id": "uuid", "texts": { "dog_name": "Burek" } }
```

All three fields required; `template_id` / `upload_id` must be UUID-shaped (`400` otherwise). Template must be published. Upload must be an active unbound `source` asset. Editable texts validated (charset/max length); unknown keys rejected; fixed template texts merged server-side. Asset `owner_ref` set to design id; attached `delete_after` moves to 7 days from activity.

`owner_token` is returned whenever `CreateBound` commits, including when post-commit preview lookup fails (empty preview). `GET` still returns `500` on asset metadata I/O errors and signs again on success.

### PATCH body

Optional `texts` and/or `upload_id`. Allowed only when status is `draft`, `generation_failed`, or `preview_ready` (else `409` `invalid_transition`). Text validation uses the **pinned** template row (any status). Replace-photo soft-deletes the previous source (keeps it from re-attach), binds the new upload under `SELECT … FOR UPDATE` with status/source predicates, and bumps retention.

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
  "artwork_preview_url": "https://…signed…",
  "artwork_preview_url_expires_at": "2026-11-10T12:01:00Z",
  "mockup_preview_url": "https://…signed…",
  "mockup_preview_url_expires_at": "2026-11-10T12:01:00Z",
  "generation_attempts": 0,
  "generation_attempts_remaining": 3,
  "max_generation_attempts": 3,
  "retry_after": 0,
  "owner_token": "base64url…"
}
```

`owner_token` appears only on create. Attempt budget fields (C7) mirror `plugins.metaldogs.generation.max_attempts_per_design`. `retry_after` is seconds of **cooldown lockout** remaining (`0` means not locked by cooldown — callers must still check `status` and `generation_attempts_remaining` before offering regenerate). Computed only from a terminal job's `finished_at`; while the latest job is active, `retry_after` stays non-zero (full cooldown). A missing/unreadable latest job with attempts &gt; 0 and cooldown enabled → `503` `unavailable` (same fail-closed rule as generate). Design `expires_at` is 30d from create / last **mutating** activity (`POST`/`PATCH`); `GET` does not slide it. Attached source `delete_after` is 7d from create/patch/replace. Replace soft-deletes the prior source row, deletes its storage object, then clears `storage_key` only on that soft-deleted row (missing object is success; other cleanup failures are logged). Source / artwork / mockup preview TTL is 60s when signing succeeds. Artwork and mockup preview fields are omitted until the design is `preview_ready` (also `approved` / `ordered`) and the latest succeeded job has live assets; signing failures soft-fail to empty like source. If the active source row is missing/soft-deleted, `upload_id` is omitted; a live source with a signer failure keeps `upload_id` and an empty preview. Asset metadata I/O errors on the source lookup are `500`.

### Errors

| HTTP | Body | When |
| --- | --- | --- |
| `400` | `bad_request` | Malformed JSON / non-UUID ids / token in query / empty patch |
| `404` | `not_found` | Unknown id / missing or wrong Bearer / expired design |
| `409` | `invalid_transition` | PATCH in non-editable status |
| `409` | `conflict` | Lost optimistic race (`updated_at` / source predicate); safe to retry |
| `422` | `template_unavailable` | Create: template not published; PATCH texts: pinned row missing |
| `422` | `upload_unavailable` | Missing/deleted/non-source upload |
| `422` | `upload_already_attached` | Upload `owner_ref` already set |
| `422` | `invalid_texts` | Charset / length / unknown or missing keys |
| `503` | `unavailable` | No DB / AssetStore / catalog |
| `500` | `internal_error` | Persistence failure |

## Generate (C4)

| Method | Path | Auth | Success |
| --- | --- | --- | --- |
| `POST` | `/api/customizer/designs/{id}/generate` | Bearer owner token | `202` job accepted |
| `GET` | `/api/customizer/generation-jobs/{id}` | Bearer (owns parent design) | `200` job status |

### Headers

- `Idempotency-Key` — **required** on generate (1–255 chars). Same key + same design returns the same job (`202`).
- `Authorization: Bearer <owner_token>` — capability token from design create. Missing/wrong → **`404`** `not_found` (not 403). Tokens must not appear in query strings (`400` `bad_request`).

### POST generate

Allowed when design status is `draft`, `generation_failed`, or `preview_ready`, the source asset is still live, and `generation_attempts < max_attempts_per_design` (default 3). Hard cap is checked before cooldown. After the first attempt, regenerations must also wait `cooldown_seconds` (default 30; `0` disables) from the latest job's `finished_at` (else `queued_at`); a missing latest job with attempts &gt; 0 → `503` `unavailable`. Creates `GenerationJob` (`queued`) scoped to `source_asset_id` + `template_version` + `texts_digest`, sets design → `generating` only when `generation_attempts` / `latest_job_id` still match the pre-bump snapshot, slides source `delete_after`, then enqueues `integration.sync.metaldogs.generate` with platform job id = product `generation_job_id` on a detached request context (preserves `request_id`). Duplicate PK → `Requeue` when the platform row is `failed`/`done`/`cancelled` or stale `processing` (Postgres queue; young `processing`/`pending` left alone). Product `claim_token` is invalidated in the same transaction as the pending update (before `Dequeue` can see it); Conflict does not bump the token a second time. Platform `Complete`/`Fail` are fenced by dequeue `attempts`; a delivery that lost the platform row returns `ErrSkipOutcome` so the worker does not ack a newer attempt. Claim/lease use database `NOW()` only. Same-key retry re-reads product status and re-enqueues when `queued` or `running` past that DB lease (`503` includes `job_id` on enqueue failure). At most one active job per design (DB partial unique). Reusing a key after replace-photo / template / texts change → `409` `conflict` with the prior `job_id`.

```json
{
  "job_id": "uuid",
  "design_id": "uuid",
  "status": "queued",
  "retry_after": 2
}
```

### GET generation-jobs

```json
{
  "id": "uuid",
  "design_id": "uuid",
  "status": "queued",
  "attempt": 1,
  "error_code": "",
  "terminal_reason": "",
  "retry_after": 2
}
```

`retry_after` is seconds while status is `queued` / `running` / `validating`; `0` when terminal. Active responses also set `Retry-After`. Worker (fake by default): claim → provider → G3 validate → store + persist artwork (`owner_ref` = design id) → G4 compose mockup (compose failures permanent; mockup Put transient) → store mockup → design `preview_ready` or `generation_failed`. Crash after artwork persist resumes without another provider call. Last queue attempt always terminals the product job (frees the active unique index). Design GET then signs `artwork_preview_url` / `mockup_preview_url` only when the succeeded job belongs to the design and assets match owner/type (C5; mockup base still a placeholder until G4 closes).

### Errors

| HTTP | Body | When |
| --- | --- | --- |
| `400` | `bad_request` | Missing/overlong Idempotency-Key; token in query |
| `404` | `not_found` | Unknown id / missing or wrong Bearer / expired design |
| `409` | `invalid_transition` (+ `job_id` when generating) | Design not eligible; includes active job id when present |
| `409` | `conflict` (+ `job_id`) | Active job / idempotency key reused with different inputs |
| `422` | `upload_unavailable` | Source missing/soft-deleted before a job is created |
| `429` | `cooldown` (+ `Retry-After` header and JSON `retry_after`) | Regenerating inside `cooldown_seconds` of the prior attempt |
| `429` | `limit_reached` (no `Retry-After`) | `generation_attempts` at configured max |
| `503` | `unavailable` | No DB / enqueue failure (body may include `job_id`; retry same Idempotency-Key → re-Enqueue) |
| `500` | `internal_error` | Persistence failure |

### Config

- `plugins.metaldogs.generation.provider` — C4 accepts only `fake` (default)
- `plugins.metaldogs.generation.max_retries` — queue retries (default 3)
- `plugins.metaldogs.generation.max_attempts_per_design` — customer regenerations (default 3)
- `plugins.metaldogs.generation.cooldown_seconds` — minimum gap between distinct attempts (default 30; `0` disables; max 86400)

## Approve and clone (C6)

| Method | Path | Auth | Success |
| --- | --- | --- | --- |
| `POST` | `/api/customizer/designs/{id}/approve` | Bearer owner token | `200` `{ design, version }` |
| `POST` | `/api/customizer/designs/{id}/clone` | Bearer (owns approved design) | `201` new draft + `owner_token` |

### POST approve

Allowed when status is `preview_ready` and the latest succeeded generation job has a live artwork asset (`owner_ref` = design, type artwork, checksum set). The handler **reads and SHA-256s** the artwork object; missing object → `422` `checksum_mismatch`; other store errors → `503` `unavailable` (no `version_id`); oversize → `422` `payload_too_large`. `ApproveBound` locks the design + artwork row, freezes a `DesignVersion`, slides design `expires_at`, and **extends** (never shortens) source/artwork/mockup `delete_after` to that expiry (artwork extend miss aborts the TX). Then enqueues `prepare_production_artwork` (platform job id = version id). Stub **fails once** (`MaxRetries=1`, `pending_spec`, not Complete). Conflict path: leave `done`/`failed`/`pending` alone (never `Admin.Retry`); reclaim `cancelled` → pending with `attempts=0` (`status='cancelled' AND type=prepare` only); young `processing` left alone; stale `processing` → pending/`run_at` only (`processing` + stale `updated_at` + type; attempts preserved for the Fail fence); zero-row re-read success for `pending`/`failed`/`done`/young `processing`, error if still stale `processing` (type mismatch / clock skew → 503). Skip enqueue when `production_status` is `ready`. Already `approved` → idempotent + re-enqueue unless ready/failed-terminal stub. Enqueue failure after commit → `503` + `version_id`. Race without approve → `409` `invalid_transition`.

```json
{
  "design": { "id": "uuid", "status": "approved", "approved_version_id": "uuid", "…": "…" },
  "version": {
    "id": "uuid",
    "design_id": "uuid",
    "template_id": "uuid",
    "template_version": 1,
    "texts": { "dog_name": "Burek", "tagline": "METAL" },
    "artwork_asset_id": "uuid",
    "artwork_checksum": "sha256:…",
    "production_status": "pending",
    "provider": "fake",
    "model": "fake",
    "prompt_version": "…",
    "approved_at": "2026-11-10T12:00:00Z"
  }
}
```

Approved designs reject `PATCH` and `POST …/generate` (`409` `invalid_transition`).

### POST clone

Allowed from `approved` (and later `ordered`). Requires source `owner_ref` = design id; verifies size ≤ upload cap, SHA-256 vs asset row, and JPEG/PNG sniff before Put-copy. Concurrency-capped like uploads. Cleanup soft-deletes then deletes the blob (`WithoutCancel` + `OnCleanupError`). Returns a new draft with a new `owner_token`. Unapproved → `409` `invalid_transition`.

### Errors (approve / clone)

| HTTP | Body | When |
| --- | --- | --- |
| `400` | `bad_request` | Token in query |
| `404` | `not_found` | Unknown id / missing or wrong Bearer / expired |
| `409` | `invalid_transition` | Approve not from `preview_ready` / race without approve; clone not from approved/ordered; PATCH/generate on approved |
| `422` | `checksum_mismatch` | Artwork/source bytes disagree with stored checksum/size, or artwork object missing |
| `422` | `payload_too_large` | Artwork over approve size cap |
| `422` | `upload_unavailable` | Clone source missing / wrong owner / bad media |
| `429` | `too_many_requests` | Clone concurrency limit |
| `503` | `unavailable` | Object store I/O (no `version_id`); or prepare enqueue failed after commit (`version_id` set — retry approve) |
| `500` | `internal_error` | Persistence failure |

## Safe delivery

No published templates ship with C1; list returns `[]` until operators `metaldogs:templates publish`. Uploads, designs, generate, approve, and clone require media + DB. Flag default off. Generate uses the fake provider unless a later slice wires a live provider. Approve enqueues prepare_production_artwork (C6 stub terminals with `pending_spec`; G5 owns reclaim of failed rows).
