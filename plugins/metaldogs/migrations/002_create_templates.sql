-- Template catalog (G2). Versions are append-only rows keyed by (slug, version).
-- Tracked as plugin:metaldogs:002_create_templates.sql
CREATE TABLE metaldogs_templates (
    id uuid PRIMARY KEY,
    slug text NOT NULL,
    status text NOT NULL CHECK (status IN ('draft', 'published', 'retired')),
    genre text NOT NULL,
    style_category text NOT NULL,
    version int NOT NULL CHECK (version >= 1),
    content_hash text NOT NULL,
    prompt_instructions jsonb NOT NULL,
    model_parameters jsonb NOT NULL,
    photo_requirements jsonb NOT NULL,
    text_fields jsonb NOT NULL,
    output_config jsonb NOT NULL,
    reference_asset_id uuid,
    thumbnail_asset_id uuid,
    reference_pattern_path text NOT NULL DEFAULT '',
    thumbnail_path text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    UNIQUE (slug, version)
);

CREATE INDEX metaldogs_templates_slug_status_idx ON metaldogs_templates (slug, status);
CREATE INDEX metaldogs_templates_status_idx ON metaldogs_templates (status);
