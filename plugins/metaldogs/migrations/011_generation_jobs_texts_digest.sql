-- C4 review2: include design texts in Idempotency-Key input scope.
-- Tracked as plugin:metaldogs:011_generation_jobs_texts_digest.sql
ALTER TABLE metaldogs_generation_jobs
    ADD COLUMN IF NOT EXISTS texts_digest text;
