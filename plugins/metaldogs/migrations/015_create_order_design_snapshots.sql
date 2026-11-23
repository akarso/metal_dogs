-- OrderDesignSnapshot (ADR-0005): one immutable copy per (order_id, variant_id).
-- production_* may change once, and only from NULL. Delete is refused.
-- Tracked as plugin:metaldogs:015_create_order_design_snapshots.sql

CREATE TABLE metaldogs_order_design_snapshots (
    order_id uuid NOT NULL,
    variant_id text NOT NULL,
    design_id uuid NOT NULL,
    design_version_id uuid NOT NULL,
    sku text NOT NULL,
    template_slug text NOT NULL,
    template_version int NOT NULL CHECK (template_version >= 1),
    texts jsonb NOT NULL,
    provider text NOT NULL,
    model text NOT NULL,
    prompt_version text NOT NULL,
    artwork_asset_id uuid NOT NULL,
    artwork_checksum text NOT NULL,
    production_asset_id uuid,
    production_checksum text,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (order_id, variant_id),
    CONSTRAINT metaldogs_order_design_snapshots_production_pair CHECK (
        (production_asset_id IS NULL AND production_checksum IS NULL)
        OR (production_asset_id IS NOT NULL AND production_checksum IS NOT NULL)
    )
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

DROP TRIGGER IF EXISTS metaldogs_order_design_snapshots_immutable_trg
    ON metaldogs_order_design_snapshots;
CREATE TRIGGER metaldogs_order_design_snapshots_immutable_trg
    BEFORE UPDATE ON metaldogs_order_design_snapshots
    FOR EACH ROW
    EXECUTE FUNCTION metaldogs_order_design_snapshots_immutable();

CREATE OR REPLACE FUNCTION metaldogs_order_design_snapshots_no_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'metaldogs_order_design_snapshots delete refused'
        USING ERRCODE = 'check_violation';
END;
$$;

DROP TRIGGER IF EXISTS metaldogs_order_design_snapshots_no_delete_trg
    ON metaldogs_order_design_snapshots;
CREATE TRIGGER metaldogs_order_design_snapshots_no_delete_trg
    BEFORE DELETE ON metaldogs_order_design_snapshots
    FOR EACH ROW
    EXECUTE FUNCTION metaldogs_order_design_snapshots_no_delete();

CREATE INDEX metaldogs_order_design_snapshots_artwork_asset_id_idx
    ON metaldogs_order_design_snapshots (artwork_asset_id)
    WHERE artwork_asset_id IS NOT NULL;

CREATE INDEX metaldogs_order_design_snapshots_production_asset_id_idx
    ON metaldogs_order_design_snapshots (production_asset_id)
    WHERE production_asset_id IS NOT NULL;
