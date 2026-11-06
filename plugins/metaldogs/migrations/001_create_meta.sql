-- Placeholder meta table for Metal Dogs (F1 skeleton).
-- Replace with a later expand/contract migration before real customizer schema.
-- Tracked as plugin:metaldogs:001_create_meta.sql
CREATE TABLE metaldogs_meta (
    key text PRIMARY KEY,
    value text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now()
);
