-- C3 review: exclusive active owner_ref; include generating in expires sweep index.
-- Tracked as plugin:metaldogs:005_design_bind_constraints.sql

-- One active (non-deleted) asset per owner_ref. Soft-deleted rows may keep owner_ref.
CREATE UNIQUE INDEX metaldogs_assets_owner_ref_uidx
    ON metaldogs_assets (owner_ref)
    WHERE owner_ref IS NOT NULL AND deleted_at IS NULL;

DROP INDEX IF EXISTS metaldogs_designs_expires_at_idx;
CREATE INDEX metaldogs_designs_expires_at_idx
    ON metaldogs_designs (expires_at)
    WHERE status IN (
        'draft', 'generating', 'generation_failed', 'preview_ready', 'approved'
    );
