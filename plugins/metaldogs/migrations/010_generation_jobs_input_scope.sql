-- C4 review: scope Idempotency-Key replay to the source + template version used at enqueue.
-- Tracked as plugin:metaldogs:010_generation_jobs_input_scope.sql
ALTER TABLE metaldogs_generation_jobs
    ADD COLUMN IF NOT EXISTS source_asset_id uuid,
    ADD COLUMN IF NOT EXISTS template_version int;
