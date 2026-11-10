-- After a successful blob delete, storage_key is cleared so O2 only sweeps rows
-- that still need object removal (soft-deleted + storage_key present).
-- Tracked as plugin:metaldogs:007_asset_storage_key_nullable.sql
ALTER TABLE metaldogs_assets
    ALTER COLUMN storage_key DROP NOT NULL;

DROP INDEX IF EXISTS metaldogs_assets_soft_deleted_blob_idx;
CREATE INDEX metaldogs_assets_soft_deleted_blob_idx
    ON metaldogs_assets (deleted_at)
    WHERE deleted_at IS NOT NULL AND storage_key IS NOT NULL;
