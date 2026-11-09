-- Asset metadata (C2). Blobs live in AssetStore; this table tracks lifecycle.
-- Tracked as plugin:metaldogs:003_create_assets.sql
CREATE TABLE metaldogs_assets (
    id uuid PRIMARY KEY,
    type text NOT NULL CHECK (type IN (
        'source', 'artwork', 'mockup', 'production', 'reference', 'thumbnail'
    )),
    storage_key text NOT NULL,
    mime_type text NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    width int NOT NULL CHECK (width > 0),
    height int NOT NULL CHECK (height > 0),
    checksum text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    delete_after timestamptz NOT NULL,
    deleted_at timestamptz
);

CREATE INDEX metaldogs_assets_delete_after_idx
    ON metaldogs_assets (delete_after)
    WHERE deleted_at IS NULL;
