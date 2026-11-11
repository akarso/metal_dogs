-- Append-only DesignVersion freeze (ADR-0005): only production_* columns may change.
-- Tracked as plugin:metaldogs:014_design_versions_append_only.sql

ALTER TABLE metaldogs_design_versions
    ADD CONSTRAINT metaldogs_design_versions_artwork_asset_id_fkey
    FOREIGN KEY (artwork_asset_id) REFERENCES metaldogs_assets (id);

CREATE OR REPLACE FUNCTION metaldogs_design_versions_append_only()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.design_id IS DISTINCT FROM OLD.design_id
        OR NEW.template_id IS DISTINCT FROM OLD.template_id
        OR NEW.template_version IS DISTINCT FROM OLD.template_version
        OR NEW.texts IS DISTINCT FROM OLD.texts
        OR NEW.artwork_asset_id IS DISTINCT FROM OLD.artwork_asset_id
        OR NEW.artwork_checksum IS DISTINCT FROM OLD.artwork_checksum
        OR NEW.provider IS DISTINCT FROM OLD.provider
        OR NEW.model IS DISTINCT FROM OLD.model
        OR NEW.prompt_version IS DISTINCT FROM OLD.prompt_version
        OR NEW.parameters IS DISTINCT FROM OLD.parameters
        OR NEW.approved_at IS DISTINCT FROM OLD.approved_at
    THEN
        RAISE EXCEPTION 'metaldogs_design_versions is append-only (production_* only)'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS metaldogs_design_versions_append_only_trg ON metaldogs_design_versions;
CREATE TRIGGER metaldogs_design_versions_append_only_trg
    BEFORE UPDATE ON metaldogs_design_versions
    FOR EACH ROW
    EXECUTE FUNCTION metaldogs_design_versions_append_only();
