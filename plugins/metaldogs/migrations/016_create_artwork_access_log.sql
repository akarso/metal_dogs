-- Append-only record of issued artwork URLs (E5). No URL and no storage key.
-- Tracked as plugin:metaldogs:016_create_artwork_access_log.sql

CREATE TABLE metaldogs_artwork_access_log (
    order_id uuid NOT NULL,
    variant_id text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('approved', 'production')),
    actor text NOT NULL CHECK (actor <> ''),
    channel text NOT NULL CHECK (channel IN ('admin', 'cli')),
    requested_at timestamptz NOT NULL
);

CREATE OR REPLACE FUNCTION metaldogs_artwork_access_log_append_only()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'metaldogs_artwork_access_log is append-only'
        USING ERRCODE = 'check_violation';
END;
$$;

DROP TRIGGER IF EXISTS metaldogs_artwork_access_log_append_only_trg
    ON metaldogs_artwork_access_log;
CREATE TRIGGER metaldogs_artwork_access_log_append_only_trg
    BEFORE UPDATE OR DELETE ON metaldogs_artwork_access_log
    FOR EACH ROW
    EXECUTE FUNCTION metaldogs_artwork_access_log_append_only();
