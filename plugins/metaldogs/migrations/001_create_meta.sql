-- Placeholder meta table for Metal Dogs (F1 skeleton).
-- Replace or extend with real customizer schema in later slices.
-- Tracked as plugin:metaldogs:001_create_meta.sql
CREATE TABLE IF NOT EXISTS metaldogs_meta (
    key text PRIMARY KEY,
    value text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now()
);
