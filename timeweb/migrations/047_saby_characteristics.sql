-- Keep the supplier payload even when no local definition exists yet. This
-- makes mapping observable without allowing Saby to invent storefront fields.
ALTER TABLE saby_nomenclature
  ADD COLUMN IF NOT EXISTS characteristics JSONB NOT NULL DEFAULT '{}'::JSONB;
