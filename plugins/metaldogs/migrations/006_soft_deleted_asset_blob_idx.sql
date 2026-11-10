-- Soft-deleted assets still hold private blobs until O2 sweep / replace cleanup.
-- Tracked as plugin:metaldogs:006_soft_deleted_asset_blob_idx.sql
CREATE INDEX metaldogs_assets_soft_deleted_blob_idx
    ON metaldogs_assets (deleted_at)
    WHERE deleted_at IS NOT NULL;
