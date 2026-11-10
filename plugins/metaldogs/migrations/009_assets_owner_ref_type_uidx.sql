-- C4 review: one live asset per (owner_ref, type) so artwork can share design id with source.
-- Tracked as plugin:metaldogs:009_assets_owner_ref_type_uidx.sql
DROP INDEX IF EXISTS metaldogs_assets_owner_ref_uidx;

CREATE UNIQUE INDEX metaldogs_assets_owner_ref_type_uidx
    ON metaldogs_assets (owner_ref, type)
    WHERE owner_ref IS NOT NULL AND deleted_at IS NULL;
