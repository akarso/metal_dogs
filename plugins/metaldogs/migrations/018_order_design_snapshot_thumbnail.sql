-- mockup_asset_id and thumbnail_asset_id are insert-only, same as the frozen columns in 015.
-- Tracked as plugin:metaldogs:018_order_design_snapshot_thumbnail.sql

ALTER TABLE metaldogs_order_design_snapshots
    ADD COLUMN mockup_asset_id uuid,
    ADD COLUMN thumbnail_asset_id uuid,
    ADD CONSTRAINT metaldogs_order_design_snapshots_preview_pair CHECK (
        (mockup_asset_id IS NULL AND thumbnail_asset_id IS NULL)
        OR (mockup_asset_id IS NOT NULL AND thumbnail_asset_id IS NOT NULL)
    );

CREATE OR REPLACE FUNCTION metaldogs_order_design_snapshots_immutable()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.order_id IS DISTINCT FROM OLD.order_id
        OR NEW.variant_id IS DISTINCT FROM OLD.variant_id
        OR NEW.design_id IS DISTINCT FROM OLD.design_id
        OR NEW.design_version_id IS DISTINCT FROM OLD.design_version_id
        OR NEW.sku IS DISTINCT FROM OLD.sku
        OR NEW.template_slug IS DISTINCT FROM OLD.template_slug
        OR NEW.template_version IS DISTINCT FROM OLD.template_version
        OR NEW.texts IS DISTINCT FROM OLD.texts
        OR NEW.provider IS DISTINCT FROM OLD.provider
        OR NEW.model IS DISTINCT FROM OLD.model
        OR NEW.prompt_version IS DISTINCT FROM OLD.prompt_version
        OR NEW.artwork_asset_id IS DISTINCT FROM OLD.artwork_asset_id
        OR NEW.artwork_checksum IS DISTINCT FROM OLD.artwork_checksum
        OR NEW.mockup_asset_id IS DISTINCT FROM OLD.mockup_asset_id
        OR NEW.thumbnail_asset_id IS DISTINCT FROM OLD.thumbnail_asset_id
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
    THEN
        RAISE EXCEPTION 'metaldogs_order_design_snapshots is append-only (production_* only)'
            USING ERRCODE = 'check_violation';
    END IF;
    IF (OLD.production_asset_id IS NOT NULL OR OLD.production_checksum IS NOT NULL)
        AND (
            NEW.production_asset_id IS DISTINCT FROM OLD.production_asset_id
            OR NEW.production_checksum IS DISTINCT FROM OLD.production_checksum
        )
    THEN
        RAISE EXCEPTION 'metaldogs_order_design_snapshots production already set'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE INDEX metaldogs_order_design_snapshots_mockup_asset_id_idx
    ON metaldogs_order_design_snapshots (mockup_asset_id)
    WHERE mockup_asset_id IS NOT NULL;

CREATE INDEX metaldogs_order_design_snapshots_thumbnail_asset_id_idx
    ON metaldogs_order_design_snapshots (thumbnail_asset_id)
    WHERE thumbnail_asset_id IS NOT NULL;
