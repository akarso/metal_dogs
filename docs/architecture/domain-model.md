# Domain model

Concepts from the project description made explicit. Names here are the names used in code. Status enums are `snake_case` and enforced in domain code (see [ADR-0006](../adr/0006-design-and-job-status-model.md)).

## Entities and relationships

```mermaid
erDiagram
    TEMPLATE ||--o{ DESIGN : "pinned by version"
    TEMPLATE ||--|| ASSET : "reference pattern, thumbnail"
    DESIGN ||--|| ASSET : "source photo"
    DESIGN ||--o{ GENERATION_JOB : "attempts"
    GENERATION_JOB ||--o| ASSET : "artwork output"
    GENERATION_JOB ||--o| ASSET : "mockup preview"
    DESIGN ||--o| DESIGN_VERSION : "approved version"
    DESIGN_VERSION ||--|| ASSET : "approved artwork"
    DESIGN_VERSION ||--o| ASSET : "production artwork"
    DESIGN_VERSION ||--o{ ORDER_DESIGN_SNAPSHOT : "ordered as"
    ORDER_DESIGN_SNAPSHOT ||--o| FULFILLMENT_RECORD : "fulfilled by"
    ORDER_DESIGN_SNAPSHOT }o--|| SHOPANDA_ORDER_LINE : "references"

    TEMPLATE {
        uuid id PK
        string slug
        enum status "draft | published | retired"
        string genre
        string style_category
        int version
        string content_hash
        jsonb prompt_instructions "server-only"
        jsonb model_parameters "server-only"
        jsonb photo_requirements
        jsonb text_fields "editable + fixed"
        jsonb output_config "canvas, print area, monochrome"
        uuid reference_asset_id FK
        uuid thumbnail_asset_id FK
        timestamptz created_at
        timestamptz published_at
    }

    DESIGN {
        uuid id PK
        uuid template_id FK
        int template_version
        enum status
        jsonb texts
        uuid source_asset_id FK
        bytea owner_token_hash "capability token, hashed"
        uuid customer_id "nullable, when logged in"
        int generation_attempts
        uuid latest_job_id FK
        uuid approved_version_id FK
        timestamptz created_at
        timestamptz updated_at
        timestamptz expires_at
    }

    GENERATION_JOB {
        uuid id PK
        uuid design_id FK
        int attempt
        enum status
        string idempotency_key
        string provider
        string model
        string prompt_version
        jsonb parameters
        uuid artwork_asset_id FK
        uuid mockup_asset_id FK
        jsonb validation_result
        string error_code
        string terminal_reason
        numeric cost
        string cost_currency
        int latency_ms
        string correlation_id
        timestamptz queued_at
        timestamptz started_at
        timestamptz finished_at
    }

    DESIGN_VERSION {
        uuid id PK
        uuid design_id FK
        uuid template_id FK
        int template_version
        jsonb texts
        uuid artwork_asset_id FK
        string artwork_checksum
        uuid production_asset_id FK
        string production_checksum
        enum production_status "pending | preparing | ready | failed"
        string provider
        string model
        string prompt_version
        timestamptz approved_at
    }

    ASSET {
        uuid id PK
        enum type "source | artwork | mockup | production | reference | thumbnail"
        string storage_key "private bucket"
        string mime_type
        bigint size_bytes
        int width
        int height
        string checksum
        timestamptz created_at
        timestamptz delete_after "retention"
        timestamptz deleted_at
    }

    ORDER_DESIGN_SNAPSHOT {
        uuid id PK
        string order_id "Shopanda"
        string order_line_id "Shopanda, unique"
        uuid design_id FK
        uuid design_version_id FK
        uuid artwork_asset_id FK
        string artwork_checksum
        uuid production_asset_id FK
        string production_checksum
        jsonb texts
        string template_slug
        int template_version
        string provider
        string model
        string prompt_version
        string variant_sku
        timestamptz created_at
    }

    FULFILLMENT_RECORD {
        uuid id PK
        uuid snapshot_id FK
        enum status
        string provider
        string external_order_id
        string tracking_number
        string last_error_code
        int attempts
        timestamptz submitted_at
        timestamptz updated_at
    }
```

Notes:

- `DESIGN_VERSION` and `ORDER_DESIGN_SNAPSHOT` are **append-only**. Nothing updates them after creation except `production_*` fields on `DESIGN_VERSION`, which move once from `pending` to a terminal state.
- Snapshot duplicates the version's data (texts, template version, checksums) deliberately so an order remains fully described even if a design is deleted under retention rules. Assets referenced by a snapshot are never swept while the snapshot's fulfilment is incomplete.
- `owner_token_hash` stores only the hash of the capability token ([ADR-0007](../adr/0007-guest-design-ownership-tokens.md)).

## Template (G2)

Templates are declarative catalog rows in `metaldogs_templates`, authored as YAML/JSON under `plugins/metaldogs/templates/<slug>.yaml` and loaded via `metaldogs:templates`.

| Field group | Role |
| --- | --- |
| `slug` + `version` | Stable identity; unique pair. Version is a positive integer, immutable once the row exists. |
| `status` | `draft` (invisible to customers) → `published` → `retired`. Publish retires any prior published version for the same slug (and leftover drafts). |
| `published_at` | Set when a row **becomes** `published` from `draft` or `retired`. Re-running publish cleanup on an already-`published` row keeps the existing stamp (`CASE WHEN status = 'published' THEN published_at ELSE now() END`). Retire does not clear the column. |
| `content_hash` | SHA-256 of the definition body (slug, genre, style, prompts, model params, photo requirements, text fields, output config, optional relative asset paths). Same hash on reload is a no-op; a changed hash inserts `version+1` as `draft`. |
| `prompt_instructions` / `model_parameters` | Server-only. Never exposed on storefront list/detail DTOs (C1). |
| `photo_requirements` | Pose enum (`head_front`, `head_angle`, `full_body_profile`, …) plus background/lighting hints for the upload step. |
| `text_fields` | Editable (must include `dog_name` with max length + charset) and fixed copy. Keys are trimmed before validate/hash/persist so padded `dog_name` cannot pass Validate while storing a spaced key. |
| `output_config` | Canvas size, monochrome flag, print area in canvas pixels. |
| `reference_asset_id` / `thumbnail_asset_id` | Nullable UUIDs until asset ingest; files may declare relative `reference_pattern_path` / `thumbnail_path` for offline validation. |

Designs pin `template_id` + `template_version` at creation ([ADR-0005](../adr/0005-immutable-order-design-snapshot.md)). Publishing workflow: [runbooks/template-publishing.md](../runbooks/template-publishing.md).

## Design status machine

```mermaid
stateDiagram-v2
    [*] --> draft : CreateDesign
    draft --> draft : UpdateTexts / ReplacePhoto
    draft --> generating : RequestGeneration (photo + texts valid, attempts < limit)
    generating --> preview_ready : job succeeded
    generating --> generation_failed : job failed (terminal)
    generation_failed --> generating : RequestGeneration (attempts < limit)
    generation_failed --> draft : ReplacePhoto / UpdateTexts
    preview_ready --> generating : RequestGeneration (regenerate, attempts < limit)
    preview_ready --> draft : ReplacePhoto / UpdateTexts
    preview_ready --> approved : Approve (freezes DesignVersion)
    approved --> ordered : OrderDesignSnapshotted
    approved --> approved : Approve (idempotent, same version)
    draft --> expired : retention sweep
    generation_failed --> expired : retention sweep
    preview_ready --> expired : retention sweep
    approved --> expired : retention sweep (never ordered)
    ordered --> [*]
    expired --> [*]

    note right of approved
        Immutable. Edits are rejected;
        "change it" clones into a new draft.
    end note
    note right of generating
        At most one active job per design
        (database uniqueness on design_id
        where status in queued/running/validating).
    end note
```

Invalid transitions return a domain error (`invalid_transition`) which the HTTP layer maps to `409`.

## GenerationJob status machine

```mermaid
stateDiagram-v2
    [*] --> queued : enqueue
    queued --> running : worker lease acquired
    running --> validating : provider returned output
    running --> queued : transient error, attempt < max (backoff)
    running --> failed : permanent error or attempts exhausted
    validating --> succeeded : output valid, assets stored
    validating --> queued : transient storage error, attempt < max
    validating --> failed : output invalid (terminal_reason = validation_failed)
    queued --> cancelled : design deleted/expired before start
    succeeded --> [*]
    failed --> [*]
    cancelled --> [*]
```

Terminal reasons (stable codes exposed to the UI in mapped form): `provider_timeout`, `provider_rejected_input`, `provider_rate_limited`, `provider_error`, `validation_failed`, `storage_error`, `attempts_exhausted`, `cancelled`, `cost_cap_reached`.

## Fulfillment status machine

```mermaid
stateDiagram-v2
    [*] --> pending : snapshot created
    pending --> awaiting_artwork : production artwork not ready
    awaiting_artwork --> pending : production artwork ready
    pending --> awaiting_approval : manual gate enabled
    awaiting_approval --> submitting : operator approves
    pending --> submitting : payment confirmed and gate disabled
    pending --> submitted_manually : operator records external order (P1)
    submitting --> submitted : provider accepted (external_order_id)
    submitting --> submission_failed : provider rejected (terminal)
    submission_failed --> submitting : operator resubmits after fix
    submitted --> in_production : provider status
    submitted_manually --> in_production : operator update
    in_production --> shipped : provider status + tracking
    submitted --> cancelled : cancellation within window
    submitted_manually --> cancelled
    shipped --> [*]
    cancelled --> [*]
```

## Value objects

| Value object | Validation |
| --- | --- |
| `DogName` | Per-template max length, allowed character set (Latin incl. Polish diacritics by default), trimmed, no control characters |
| `TemplateVersion` | Positive integer, immutable once published |
| `Checksum` | SHA-256 hex, computed over stored bytes |
| `PrintArea` | Width/height in mm plus pixel canvas; validated against product print spec |
| `Attempts` | Bounded by template or global limit |
| `RetentionPolicy` | Durations per asset type and design status; pure decision function |
| `Money` | Provider cost with currency; never floats in persistence |

## Retention policy (initial, to be confirmed in O1/O2)

| Subject | Condition | Keep for |
| --- | --- | --- |
| Upload not attached to a design | — | 2 hours |
| Source photo | design never ordered | 7 days from last activity |
| Artwork and mockup previews | design never ordered | 7 days from last activity |
| Design row | never ordered | 30 days, then `expired` and purged |
| Source photo | design ordered | until fulfilment `shipped` + 14 days |
| Approved and production artwork | design ordered | per legal/warranty policy (decision in O2) |
| Snapshot row | always | with the order |
