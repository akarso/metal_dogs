-- GenerationJob aggregate (C4 / ADR-0003 / ADR-0006).
-- Tracked as plugin:metaldogs:008_create_generation_jobs.sql
CREATE TABLE metaldogs_generation_jobs (
    id uuid PRIMARY KEY,
    design_id uuid NOT NULL REFERENCES metaldogs_designs (id),
    attempt int NOT NULL CHECK (attempt >= 1),
    status text NOT NULL CHECK (status IN (
        'queued', 'running', 'validating', 'succeeded', 'failed', 'cancelled'
    )),
    idempotency_key text,
    provider text NOT NULL,
    model text NOT NULL DEFAULT '',
    prompt_version text NOT NULL DEFAULT '',
    parameters jsonb NOT NULL DEFAULT '{}'::jsonb,
    artwork_asset_id uuid,
    mockup_asset_id uuid,
    validation_result jsonb,
    error_code text,
    terminal_reason text,
    cost numeric,
    cost_currency text,
    latency_ms int,
    correlation_id text,
    queued_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz
);

-- At most one active job per design (pipeline mutex).
CREATE UNIQUE INDEX metaldogs_generation_jobs_active_design_uidx
    ON metaldogs_generation_jobs (design_id)
    WHERE status IN ('queued', 'running', 'validating');

-- Same Idempotency-Key for the same design returns the same job.
CREATE UNIQUE INDEX metaldogs_generation_jobs_idempotency_uidx
    ON metaldogs_generation_jobs (design_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

CREATE INDEX metaldogs_generation_jobs_design_id_idx
    ON metaldogs_generation_jobs (design_id);
