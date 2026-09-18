-- Mirror the Saby catalogue tree independently from storefront products.
-- Folder IDs come from Retail hierarchicalId and survive renames/moves.
CREATE TABLE IF NOT EXISTS saby_catalog_folders (
    saby_id TEXT PRIMARY KEY,
    parent_saby_id TEXT,
    name TEXT NOT NULL,
    path TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    seen_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    missing_since TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS saby_catalog_folders_parent_idx
    ON saby_catalog_folders(parent_saby_id)
    WHERE missing_since IS NULL;

CREATE INDEX IF NOT EXISTS saby_catalog_folders_path_idx
    ON saby_catalog_folders USING GIN(path);

ALTER TABLE saby_nomenclature
    ADD COLUMN IF NOT EXISTS folder_saby_id TEXT;

CREATE INDEX IF NOT EXISTS saby_nomenclature_folder_idx
    ON saby_nomenclature(folder_saby_id)
    WHERE missing_since IS NULL;
