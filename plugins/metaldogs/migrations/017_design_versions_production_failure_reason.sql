-- Failure reason for production artwork (G5a). Append-only trigger from 014
-- lists frozen columns, so this production_* column may change without a new trigger.
-- Tracked as plugin:metaldogs:017_design_versions_production_failure_reason.sql

ALTER TABLE metaldogs_design_versions
    ADD COLUMN production_failure_reason text;
