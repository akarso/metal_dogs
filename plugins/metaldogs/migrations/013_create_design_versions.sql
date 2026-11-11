-- DesignVersion freeze at approve (C6 / ADR-0005 / ADR-0006).
-- Tracked as plugin:metaldogs:013_create_design_versions.sql
CREATE TABLE metaldogs_design_versions (
    id uuid PRIMARY KEY,
    design_id uuid NOT NULL REFERENCES metaldogs_designs (id),
    template_id uuid NOT NULL,
    template_version int NOT NULL CHECK (template_version >= 1),
    texts jsonb NOT NULL,
    artwork_asset_id uuid NOT NULL,
    artwork_checksum text NOT NULL,
    production_asset_id uuid,
    production_checksum text,
    production_status text NOT NULL CHECK (production_status IN (
        'pending', 'preparing', 'ready', 'failed'
    )),
    provider text NOT NULL DEFAULT '',
    model text NOT NULL DEFAULT '',
    prompt_version text NOT NULL DEFAULT '',
    parameters jsonb NOT NULL DEFAULT '{}'::jsonb,
    approved_at timestamptz NOT NULL DEFAULT now()
);

-- At most one approved version per design.
CREATE UNIQUE INDEX metaldogs_design_versions_design_id_uidx
    ON metaldogs_design_versions (design_id);

ALTER TABLE metaldogs_designs
    ADD CONSTRAINT metaldogs_designs_approved_version_id_fkey
    FOREIGN KEY (approved_version_id) REFERENCES metaldogs_design_versions (id);
