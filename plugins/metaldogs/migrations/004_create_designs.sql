-- Design aggregate + ownership token hash (C3 / ADR-0006 / ADR-0007).
-- Tracked as plugin:metaldogs:004_create_designs.sql
CREATE TABLE metaldogs_designs (
    id uuid PRIMARY KEY,
    template_id uuid NOT NULL,
    template_version int NOT NULL CHECK (template_version >= 1),
    status text NOT NULL CHECK (status IN (
        'draft', 'generating', 'generation_failed', 'preview_ready',
        'approved', 'ordered', 'expired'
    )),
    texts jsonb NOT NULL,
    source_asset_id uuid NOT NULL,
    owner_token_hash bytea NOT NULL,
    customer_id uuid,
    generation_attempts int NOT NULL DEFAULT 0 CHECK (generation_attempts >= 0),
    latest_job_id uuid,
    approved_version_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX metaldogs_designs_expires_at_idx
    ON metaldogs_designs (expires_at)
    WHERE status IN ('draft', 'generation_failed', 'preview_ready', 'approved');

CREATE INDEX metaldogs_designs_source_asset_id_idx
    ON metaldogs_designs (source_asset_id);
