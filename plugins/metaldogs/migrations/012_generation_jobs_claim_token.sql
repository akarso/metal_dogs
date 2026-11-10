-- C4 review3: fence overlapping deliveries with a claim token.
-- Tracked as plugin:metaldogs:012_generation_jobs_claim_token.sql
ALTER TABLE metaldogs_generation_jobs
    ADD COLUMN IF NOT EXISTS claim_token bigint NOT NULL DEFAULT 0;
