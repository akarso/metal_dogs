# Customizer HTTP API

Public surface under `/api/customizer/*` (plugin flag `plugins.metaldogs.enabled`). Guest responses use platform `Cache-Control: no-store` (F1 prefix list), except the order-thumbnail GET. Not k8s probes — use `/healthz` / `/readyz`.

## Enablement & health

Exact `/api/customizer` is not a route. The platform answers with a plain 404, which may be cached. Every registered `/api/customizer/*` route except the order-thumbnail GET is `Cache-Control: no-store` on guest GET and HEAD (`pluginsdk.NoCache`). `GET /api/customizer/order-thumbnails/{token}` is registered without `NoCache`. A `200` sets `Cache-Control: private, max-age=86400`. A `404` sets `Cache-Control: no-store`.

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| `GET` | `/api/customizer/enabled` | public | `{"status":"ok","plugin":"metaldogs"}` |
| `GET` | `/api/customizer/health` | public | `Host.Jobs()` bound (`CapJobs`) only; `503` + generic `degraded` when not ready |
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

Handlers do not set public `max-age`. Platform middleware forces `Cache-Control: no-store` on each registered `/api/customizer/*` route. Exact `/api/customizer` is not on that denylist.

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

Platform `AuthMiddleware` classifies a Bearer that is not a customer JWT (`ErrNotCustomerToken`: wrong shape, not three segments) as anonymous for the platform: the storefront session cookie is used when it is valid, otherwise the request is a guest. Owner tokens are raw base64url with no dots, so they take that path on `/api/customizer` and `/api/customizer/*`. A Bearer with two dots that fails signature or expiry is `401`, and the session cookie is not consulted. Scheme match is case-insensitive. Token in query string is rejected (`400`). Wrong/missing Bearer, unknown id, or past `expires_at` → **`404` `not_found`** (never `403`). Token is never logged; hash never returned.

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

`owner_token` appears only on create/clone. Attempt budget fields (C7) mirror `plugins.metaldogs.generation.max_attempts_per_design`. `retry_after` is seconds of **cooldown lockout** remaining (`0` means not locked by cooldown — callers must still check `status` and `generation_attempts_remaining` before offering regenerate). Computed only from a terminal job's `finished_at`; while the latest job is active, `retry_after` equals the full `cooldown_seconds` window (not time remaining until terminal). A missing/unreadable latest job (or terminal job without `finished_at`, or a job whose `design_id` does not match) with attempts &gt; 0 and cooldown enabled → `503` `unavailable` on `GET`/`PATCH`/approve view (same fail-closed rule as generate). `PATCH` runs that cooldown check **before** `UpdateEditable` / `ReplaceSource` and reuses the preflight `retry_after` for the response (no second cooldown job read). If a rare post-commit preview/source view failure still happens, the error body includes the committed `design` with that preflight `retry_after`. Approve that commits but cannot build the same fail-closed view as `GET` (cooldown lookup, or generation preview job/artwork/mockup I/O — including when `cooldown_seconds` is `0`) returns `503` `unavailable` with `version_id` and no `design` field. Job/preview I/O failures wrap the cause for server logs when present and set `design_id` / `job_id` on the error (`503` body stays `unavailable`, may include those ids). Create/clone soft-fail preview signing only so `owner_token` still returns. Design `expires_at` is 30d from create / last **mutating** activity (`POST`/`PATCH`); `GET` does not slide it. Attached source `delete_after` is 7d from create/patch/replace. Replace soft-deletes the prior source row, deletes its storage object, then clears `storage_key` only on that soft-deleted row (missing object is success; other cleanup failures are logged). Source / artwork / mockup preview TTL is 60s when signing succeeds. Artwork and mockup preview fields are omitted until the design is `preview_ready` (also `approved` / `ordered`) and the latest succeeded job has live assets; signer failures soft-fail to empty like source. If the active source row is missing/soft-deleted, `upload_id` is omitted; a live source with a signer failure keeps `upload_id` and an empty preview. Source asset metadata I/O errors are `500` on both `GET` and approve (approve does not promote them to `503`); generation preview job/artwork/mockup I/O is `503`.

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
| `503` | `unavailable` | No DB / AssetStore / catalog; cooldown lookup fail-closed; generation preview job/artwork/mockup I/O; approve view after commit (`version_id` set); rare `PATCH` post-commit view failure (body includes committed `design`) |
| `500` | `internal_error` | Persistence failure / source asset I/O on `GET` or approve view |

## Generate (C4)

| Method | Path | Auth | Success |
| --- | --- | --- | --- |
| `POST` | `/api/customizer/designs/{id}/generate` | Bearer owner token | `202` job accepted |
| `GET` | `/api/customizer/generation-jobs/{id}` | Bearer (owns parent design) | `200` job status |

### Headers

- `Idempotency-Key` — **required** on generate (1–255 chars). Same key + same design returns the same job (`202`).
- `Authorization: Bearer <owner_token>` — capability token from design create. Missing/wrong → **`404`** `not_found` (not 403). Tokens must not appear in query strings (`400` `bad_request`).

### POST generate

Allowed when design status is `draft`, `generation_failed`, or `preview_ready`, the source asset is still live, and `generation_attempts < max_attempts_per_design` (default 3). Hard cap is checked before cooldown. After the first attempt, regenerations must also wait `cooldown_seconds` (default 30; `0` disables) from the latest terminal job's `finished_at` only (active latest job → full-window `429` `cooldown` with `job_id`; terminal job missing `finished_at` → `503` `unavailable`). A missing/unreadable latest job with attempts &gt; 0 → `503` `unavailable`. Creates `GenerationJob` (`queued`) scoped to `source_asset_id` + `template_version` + `texts_digest`, sets design → `generating` only when `generation_attempts` / `latest_job_id` still match the pre-bump snapshot, slides source `delete_after`, then enqueues `integration.sync.metaldogs.generate` with platform job id = product `generation_job_id` on a detached request context (preserves `request_id`). Duplicate PK → `Requeue` when the platform row is `failed`/`done`/`cancelled` or stale `processing` (Postgres queue; young `processing`/`pending` left alone). Product `claim_token` is invalidated in the same transaction as the pending update (before `Dequeue` can see it); Conflict does not bump the token a second time. Platform `Complete`/`Fail` are fenced by dequeue `attempts`; a delivery that lost the platform row returns `ErrSkipOutcome` so the worker does not ack a newer attempt. Claim/lease use database `NOW()` only. Same-key retry re-reads product status and re-enqueues when `queued` or `running` past that DB lease (`503` includes `job_id` on enqueue failure). At most one active job per design (DB partial unique). Reusing a key after replace-photo / template / texts change → `409` `conflict` with the prior `job_id`.

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
| `429` | `cooldown` (+ `Retry-After` header and JSON `retry_after`; `job_id` when known) | Regenerating inside `cooldown_seconds` of the prior attempt's `finished_at`, or latest job still active |
| `429` | `limit_reached` (no `Retry-After`) | `generation_attempts` at configured max |
| `503` | `unavailable` | No DB / enqueue failure (body may include `job_id`; retry same Idempotency-Key → re-Enqueue); missing latest job; terminal latest job without `finished_at` |
| `500` | `internal_error` | Persistence failure |

### Config

- `plugins.metaldogs.generation.provider` — C4 accepts only `fake` (default). Env `SHOPANDA_PLUGINS_METALDOGS_GENERATION_PROVIDER` (Extra key `generation_provider`, dotted alias `generation.provider`)
- `plugins.metaldogs.generation.max_retries` — queue retries (default 3; `<=0` becomes 3). Env `SHOPANDA_PLUGINS_METALDOGS_GENERATION_MAX_RETRIES`. A non-integer fails Init
- `plugins.metaldogs.generation.max_attempts_per_design` — customer regenerations (default 3; `<=0` becomes 3). Env `SHOPANDA_PLUGINS_METALDOGS_GENERATION_MAX_ATTEMPTS_PER_DESIGN`
- `plugins.metaldogs.generation.cooldown_seconds` — minimum gap between distinct attempts (default 30; `0` disables; max 86400). Env `SHOPANDA_PLUGINS_METALDOGS_GENERATION_COOLDOWN_SECONDS`
- Nested YAML `generation:` (`provider`, `max_retries`, `max_attempts_per_design`, `cooldown_seconds`) loads the same fields. `plugins.metaldogs.enabled` stays `SHOPANDA_PLUGINS_METALDOGS_ENABLED` (default false; `1`/`true`/`yes`)

## Approve and clone (C6)

| Method | Path | Auth | Success |
| --- | --- | --- | --- |
| `POST` | `/api/customizer/designs/{id}/approve` | Bearer owner token | `200` `{ design, version }` |
| `POST` | `/api/customizer/designs/{id}/clone` | Bearer (owns approved design) | `201` new draft + `owner_token` |

### POST approve

Allowed when status is `preview_ready` and the latest succeeded generation job has a live artwork asset (`owner_ref` = design, type artwork, checksum set). The handler **reads and SHA-256s** the artwork object; missing object → `422` `checksum_mismatch`; other store errors → `503` `unavailable` (no `version_id`); oversize → `422` `payload_too_large`. `ApproveBound` locks the design + artwork row, freezes a `DesignVersion`, slides design `expires_at`, and **extends** (never shortens) source/artwork/mockup `delete_after` to that expiry (artwork extend miss aborts the TX). Then enqueues `prepare_production_artwork` (platform job id = version id, `MaxRetries=3`, [CR-36](../cr-rounds/CR-36-g5a-production-artwork.md)). With no `production_spec` the handler errors `pending_spec` and leaves the version `pending` (that delivery sets `max_retries = attempts`). With a spec it stores the production file or sets `failed` plus `production_failure_reason`. Conflict path: leave `done`/`failed`/`pending` alone (never `Admin.Retry`); reclaim `cancelled` → pending with `attempts=0` (`status='cancelled' AND type=prepare` only); young `processing` left alone; stale `processing` → pending/`run_at` only (`processing` + stale `updated_at` + type; attempts preserved for the Fail fence); zero-row re-read success for `pending`/`failed`/`done`/young `processing`, error if still stale `processing` (type mismatch / clock skew → 503). Skip enqueue when `production_status` is `ready`. Already `approved` → idempotent + re-enqueue unless ready/failed-terminal stub. Enqueue failure after commit → `503` + `version_id`. Race without approve → `409` `invalid_transition`.

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
| `503` | `unavailable` | Object store I/O before commit (no `version_id`); prepare enqueue or generation-preview view I/O after commit (`version_id` set — retry approve; version may already exist) |
| `500` | `internal_error` | Persistence failure; source metadata I/O on the post-commit approve view (no `version_id` — retry approve is still safe because the version may already exist) |

## Add to cart (E2)

| Method | Path | Auth | Success |
| --- | --- | --- | --- |
| `POST` | `/api/customizer/designs/{id}/add-to-cart` | Bearer owner token | `200` `{ "cart_url": "/cart" }` |

Bearer handling matches Designs (C3): an owner token has no dots, so `AuthMiddleware` treats `ErrNotCustomerToken` as the storefront session or a guest; a two-dot Bearer that fails validation is `401`. The owner token is the `Authorization: Bearer` header (ADR-0007). A token in the query string is `400` `bad_request`. The route reads and writes the storefront session cart cookie `shopanda_storefront_cart` (HttpOnly, `Path=/`, 30 days, `SameSite=Lax`). It creates that cart when the cookie is missing or does not name an active cart. The cart id is not a request field and is not in the JSON body. After a successful add the cookie is the cart; the design token is not required on later cart calls.

### Body

```json
{ "variant_id": "uuid" }
```

`variant_id` is a variant of the custom tee (slug `mdogs-tee`). Unknown JSON fields are rejected. A second JSON value after the object is `422` `unprocessable`. Body cap is 1 MiB.

### Success

`200` and `Set-Cookie` for the session cart. `cart_url` is `/cart`. A new line has quantity `1`. The public cart response does not show `metaldogs.design_id`. Repeating the same design and the same variant returns `200` and does not change quantity. The same design on another variant adds a second line. Quantity above one is changed on the Shopanda cart page; this route never raises it. The design's status, versions, and expiry are unchanged.

The field is private, plugin-only write, snapshot storage, UUID-shaped. Shopper order JSON, the confirmation email, and the storefront order pages omit it. Checkout still copies the stored value onto the order line. The trusted-write context is created only around `AddItem`. `EnsureStorefrontCart` and `CatalogPort.FindVariants` run without that grant. The field code and the trust marker are fixed in code.

Snapshot storage means Shopanda copies `metaldogs.design_id` onto the order line at checkout, before `order.created` at this pin. E4b reads that copy through `Host.Orders()` when the flag is on ([CR-34](../cr-rounds/CR-34-e4b-order-source.md)). The flag defaults off. This route does not create the order snapshot. An `ordered` design stays `409` `invalid_transition` here; re-order is clone, then approve.

### Errors

| HTTP | Body | When |
| --- | --- | --- |
| `400` | `bad_request` | Owner token in the query string |
| `403` | `forbidden_private_field` | Any path other than this route tries to set `metaldogs.design_id` (public cart API add and update, SSR cart pages, admin values API). Nothing is stored. A private field is refused before the plugin-only check |
| `404` | `not_found` | Unknown design, non-UUID design id, missing or wrong Bearer, expired design |
| `409` | `invalid_transition` | Design exists and is owned but is not `approved` (including `ordered`) |
| `409` | `conflict` | This variant already has a different design, or it is already a custom-tee line with no `metaldogs.design_id`. The cart is unchanged. The customer removes that line before a design can be attached |
| `422` | `unprocessable` | Malformed JSON, unknown fields, trailing data after the object, `variant_id` missing or not a UUID, variant is not part of the custom tee |
| `503` | `unavailable` | Design service or session-cart port is not configured |

`cart.validate` (plugin enabled) fails the mutation when the `cart` payload is present and is not a cart view (logged as `metaldogs.cart.validate.payload`). It blocks a custom-tee line with no design id (`metaldogs.design_required`, "design required"), an unknown or removed design (`metaldogs.design_not_found`, "design not found"), an expired design (`metaldogs.design_expired`, "design has expired"), or a design that is neither `approved` nor `ordered` (`metaldogs.design_not_approved`, "design is not approved"). A variant whose parent is outside the catalog read scope, or whose loaded slug is blank, is `metaldogs.product_unavailable` ("product is not available") — not a skip. That issue uses the same line scope as the design checks: a new or changed line, and every such line when the cart snapshot is unchanged (cart read and checkout). Changing, adding, or removing a different line does not raise it for an unchanged line. The cart API surfaces a blocking issue as `422` `cart_validation_failed`. The hook does not check ownership. A loaded product with a different slug is left alone. `metaldogs.design_mismatch` is reserved for a template-to-product check and is not emitted until X2. With the plugin enabled and no design store, a custom-tee line with no design id still blocks, and a custom-tee line that has a design id blocks as not found.

### Limits

- Guest cart merge into a customer cart copies `metaldogs.design_id` without the add path or the hook. E3 re-validates at checkout.
- A dotless owner token is a guest (`AuthMiddleware` + `ValidatingTokenParser`). `EnsureStorefrontCart` then uses the guest cookie. A signed-in customer id on the request (PR-1554) takes `EnsureActive` and does not write that cart id into the guest cookie. This route's bearer is the owner token, so the supported call is still the guest cart.
- Rate limiting for this route is O4.
- `cart.validate` reads the design id of the line being added from the `pending_line` hook key (plugin-only fields are already redacted to the registrant). Stored line extensions cover later reads, when `pending_line` is absent. The route does not write `metaldogs.design_id` before `AddItem`. `UpdatedBy` on the write is the cart's customer id when that id is set, otherwise `guest`. Two concurrent adds of different designs on the same cart and size can still both pass. E3 re-validates at checkout.
- A failed `AddItem` after `EnsureStorefrontCart` calls `DiscardEmptyGuestCart` with that cart id only when the Ensure snapshot had no lines and no customer id. The cookie set for a new empty guest cart is cleared. A failed add on a cart that already has lines, or on an empty customer cart, leaves that cart in place. Invalid design and an unreadable variant are refused before `EnsureStorefrontCart`, so they do not create a cart.
- Variant-to-product checks use one `CatalogPort.FindVariants` call and `extapi.ClassifyProductSlug`. Match runs the design checks. A different loaded slug is skipped. Unknown (parent outside read scope, or a blank loaded slug) is a blocking issue on the same lines as those design checks.

## Checkout (E3)

`POST /api/v1/checkout` runs the plugin step `metaldogs_design_sale` at `before:reserve_inventory` when the plugin is enabled. The step loads the cart with `Host.Cart()` and the plugin clock, and applies `validateDesignLine` (the same sale check as `cart.validate`) to every custom-tee line (slug `mdogs-tee`). `approved` and `ordered` designs that have not expired pass. A loaded product with another slug is ignored. An unknown product (parent outside the catalog read, or a blank slug) fails the step. The step is absent when the plugin is disabled. Checkout does not call `cart.validate`.

The step returns `pluginsdk.ValidationError("Design no longer available", WithClientCode("plugin.metaldogs.design_unavailable"), WithDetails(...))`. `details.lines` lists every failing line as `{variant_id, code}`. The line code is the sale-check code without the `metaldogs.` prefix: `design_required`, `design_not_found`, `design_expired`, `design_not_approved`, `product_unavailable`. `details.retryable` is `false`. The cart hook still emits the prefixed codes. A cart, catalog, or design-store failure, or a missing cart id, returns `pluginsdk.UnavailableError("Checkout validation unavailable", WithClientCode("plugin.metaldogs.validation_unavailable"), WithDetails({"retryable": true}))`. The cause is logged as `metaldogs.checkout.unavailable` and is not copied into the body. A design refusal is one `metaldogs.checkout.design_refused` line: cart id, variant ids, and those line codes. Shopanda maps the step error in `internal/platform/httpx/response.go`: client code becomes `error.code`, details are kept, validation stays `422`, unavailable stays `503`. The workflow wrap (`checkout: step %q failed: %w`) does not replace that body.

Measured by `TestCheckout_DesignSale` on an isolated database, manual payment, no network. A passing checkout is `201` and `data.order` contains the line. A logged-in cart (non-empty `customer_id`) is refused with the same body as a guest cart. Two failing lines are both listed.

| HTTP | `error.code` | `error.message` | When |
| --- | --- | --- | --- |
| `201` | — | — | Every custom-tee line passes, or the cart has no custom-tee line. `data.order.id` is set. The same design on two sizes is two lines |
| `422` | `plugin.metaldogs.design_unavailable` | `Design no longer available` | A custom-tee line fails the sale check (no design id, missing or removed design, expired, not `approved` or `ordered`) or the product classification is unknown. `details.lines` has one entry per failing line. `details.retryable` is `false`. No order row and no reservation |
| `503` | `plugin.metaldogs.validation_unavailable` | `Checkout validation unavailable` | The cart port, catalog, or design store cannot be read, or the cart id is missing. `details.retryable` is `true`. The cause stays in the log. No order row and no reservation |

Captured from `TestCheckout_DesignSale` (two bad lines, then a cart-port failure):

```json
{"data":null,"error":{"code":"plugin.metaldogs.design_unavailable","message":"Design no longer available","details":{"lines":[{"code":"design_expired","variant_id":"a1d29108-fbeb-4f27-9bfc-b0691a58b3fb"},{"code":"design_not_approved","variant_id":"c971f21d-35b6-47cb-a357-55986aa421f5"}],"retryable":false}}}
```

```json
{"data":null,"error":{"code":"plugin.metaldogs.validation_unavailable","message":"Checkout validation unavailable","details":{"retryable":true}}}
```

`POST /checkout/confirm` shows `apperror.Message` for this `422` and for this `503` (`storefrontCheckoutErrorMessage`). SSR confirm (PR-1598) renders Stripe.js on `checkout_confirm` after POST then 303 then GET. That path has no recorded maintainer walkthrough. Stripe test mode is not part of the measurement. Checklist: [`docs/runbooks/checkout-manual-verification.md`](../runbooks/checkout-manual-verification.md).

## Order artwork (E5)

Registered only when the plugin is enabled and `plugins.metaldogs.order_snapshot.enabled` is true. Same flag as the snapshot jobs. Off: this route and `metaldogs.snapshots.read` are absent. Permission is granted to role `admin` only.

`GET /api/v1/admin/orders/{order_id}/artwork` requires `metaldogs.snapshots.read`. The handler is `httpapi.OrderArtwork`. It calls `application.OrderArtworkAccess.Access` with channel `admin`. The actor is `pluginsdk.CustomerID(r)`: the request identity's user id (`auth.StorefrontActorID`). A guest is `401` from `admin.RequirePermission` before the handler. On `/api/v1/admin/*` a staff Bearer is that identity; a storefront session cookie does not replace it (`storefront.AuthMiddleware`).

`200` body (no `data` wrapper):

```json
{
  "order_id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
  "lines": [
    {
      "variant_id": "11111111-1111-4111-8111-111111111111",
      "sku": "TEE-BLK-M",
      "template_slug": "black-tee",
      "template_version": 3,
      "texts": {"dog_name": "Burek"},
      "provider": "openai",
      "model": "gpt-image",
      "prompt_version": "prompt-v7",
      "approved": {"url": "<signed url, 300s>", "expires_at": "2026-11-23T16:05:00Z", "checksum": "sha256:approved"},
      "production": {"url": "<signed url, 300s>", "expires_at": "2026-11-23T16:05:00Z", "checksum": "sha256:production"}
    }
  ]
}
```

`production` is JSON `null` when `production_asset_id` is empty. An existing order with no snapshot rows is `200` and `"lines":[]`. The URL TTL is 300s (`OrderArtworkTTL`), not caller-controlled, and still capped by `MaxSignedGetTTL` (15m). `expires_at` is that deadline. Approved artwork is inline only for `.jpg` / `.jpeg` / `.png` / `.webp`. Production artwork is always an attachment, including a `.png` key. The body has no storage key. `200` and the handler errors (`400`, `404`, `503`) send `Cache-Control: no-store`. `RegisterAdminRoute` has no `pluginsdk.NoCache()` option, so `httpapi.OrderArtwork` sets the header. `401` and `403` are `RequirePermission` before that handler.

The shell command `metaldogs:order-artwork <order_id>` prints the same JSON (actor `cli`, channel `cli`). It does not check `metaldogs.snapshots.read`. Flag off: the command is not registered, and the handler exits non-zero with `plugins.metaldogs.order_snapshot.enabled is false`.

Each signed URL appends one row to `metaldogs_artwork_access_log` before the response is returned. Columns: `order_id`, `variant_id`, `kind` (`approved`|`production`), `actor`, `channel` (`admin`|`cli`), `requested_at`. No URL and no storage key. Update and delete are rejected. If that write fails, the HTTP status is `503` and the body has no URL. The log line is `metaldogs.order_artwork.access` with `order_id` and `actor` only.

Measured by `TestOrderArtworkAdminRoute` (isolated Postgres, `RequirePermission` + `AuthMiddleware`, fake clock). `Fetch` returns the bytes at issue time. After `FakeClock` advances `OrderArtworkTTL + 1s`, `Fetch` fails. `TestOrderArtworkCLI` is the same service with actor `cli`.

| HTTP | `error.code` | `error.message` | When |
| --- | --- | --- | --- |
| `200` | — | — | Order exists. `lines` may be empty. `production` may be `null` |
| `400` | `plugin.metaldogs.invalid_order_id` | `order id is not a uuid` | Path id is not a UUID |
| `401` | `unauthorized` | `authentication required` | Guest. Platform body from `RequirePermission`. No `plugin.metaldogs` code |
| `403` | `forbidden` | `insufficient permissions` | Authenticated role without `metaldogs.snapshots.read`. No audit row |
| `404` | `plugin.metaldogs.order_not_found` | `not found` | Order id is a UUID and `FindByID` misses. No audit row |
| `503` | `plugin.metaldogs.artwork_unavailable` | `order artwork access failed` | Store, asset, or audit write failed. Body has no URL and no storage key. No new audit row |

Plugin error shape: `{"data":null,"error":{"code":"…","message":"…"}}`.

## Order thumbnail (E6a)

Registered only when the plugin is enabled, `plugins.metaldogs.order_snapshot.enabled` is true, and `plugins.metaldogs.thumbnail_signing_key` is set. The key has no default and must be a freshly generated random secret, not a passphrase. Shorter than 32 characters fails plugin Init and the error does not include the value. Absent key: this route is not registered and `(*Plugin).OrderThumbnailURL` returns an empty string. No auth and no owner token.

`GET /api/customizer/order-thumbnails/{token}` is `httpapi.OrderThumbnail`. The token is `base64url(order_id|variant_id)` + `.` + `base64url(HMAC-SHA256(key, "order-thumbnail:" + order_id + "|" + variant_id))` (`encoding/base64` raw URL encoding). It does not expire. Verification uses `hmac.Equal`. The handler loads the snapshot, reads the thumbnail through `AssetStore`, and writes the bytes. The handler does not log the token. The platform HTTP access log records the matched ServeMux pattern (`GET /api/customizer/order-thumbnails/{token}`), not the raw path (PR-1604, [CR-39](../cr-rounds/CR-39-upg13-pin-7ab17606.md)). A reverse proxy that logs the raw URL can still record the token. A snapshot, asset, or object read error is still `404` and logs `metaldogs.order_thumbnail` once, with `order_id`, `variant_id`, and `cause` (`snapshot`, `asset`, or `object`). The error text is not logged.

`200` headers: `Content-Type` is one of `image/jpeg`, `image/png`, `image/webp` (the stored MIME, lower-cased); `Content-Disposition: inline`; `Cache-Control: private, max-age=86400`; `X-Content-Type-Options: nosniff`. Any other stored MIME is the same `404` as a missing object. The route is not on the `NoCache` denylist, so platform middleware does not replace that `Cache-Control`.

`404` body is only `{"error":"not_found"}`, with `Content-Type: application/json`, `Cache-Control: no-store`, and `X-Content-Type-Options: nosniff`. That body is used for a bad or truncated token, a token whose order id or variant id was swapped, an unknown snapshot, a snapshot with no thumbnail, a missing object, and a MIME outside the allowlist. Headers and body contain no storage key and no URL.

`(*Plugin).OrderThumbnailURL(ctx, orderID, variantID)` returns `<public_base_url>/api/customizer/order-thumbnails/<token>` when that snapshot has a thumbnail. It returns an empty string when the flag is off, the key is absent, the line is missing, or the thumbnail id is empty. `plugins.metaldogs.public_base_url` is an absolute `http` or `https` origin with no query or fragment. Empty, when a thumbnail exists, is an error. The plugin cannot read `server.public_base_url` (`Host.ConfigValue` is plugin-scoped). Nothing in email or a storefront page calls this function.

Measured by `TestOrderThumbnailHTTP` (isolated Postgres, in-memory store). The link from `OrderThumbnailURL` is the request the handler accepts.

| HTTP | Body | When |
| --- | --- | --- |
| `200` | image bytes | Token verifies and the snapshot thumbnail object exists with an allowlisted MIME |
| `404` | `{"error":"not_found"}` | Bad, truncated, or swapped token; unknown line; no thumbnail; missing object; MIME outside the allowlist. Same body for each |

## Safe delivery

No published templates ship with C1; list returns `[]` until operators `metaldogs:templates publish`. Uploads, designs, generate, approve, clone, and add-to-cart require media + DB. The design field and `cart.validate` register when the plugin is enabled, including when media is absent. Flag default off. Generate uses the fake provider unless a later slice wires a live provider. Approve enqueues prepare_production_artwork (no spec: `pending_spec`, version stays `pending`; spec set: G5a writes the file, [CR-36](../cr-rounds/CR-36-g5a-production-artwork.md)). Reclaim of failed prepare rows is unchanged. Add to cart uses the same plugin flag. The E1 seed stays draft with stock 0; a buyable tee is published and stocked outside that seed.
