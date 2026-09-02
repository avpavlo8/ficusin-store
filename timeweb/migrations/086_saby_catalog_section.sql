-- Keep the Saby catalogue branch with each local nomenclature card. Sales
-- diagnostics can then fail closed and show only the owner's chosen branch,
-- instead of guessing from product names or leaking soil and pots into the
-- indoor-plant matching queue.
ALTER TABLE saby_nomenclature
  ADD COLUMN IF NOT EXISTS section_path TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];

CREATE INDEX IF NOT EXISTS saby_nomenclature_section_path_idx
  ON saby_nomenclature USING GIN(section_path);

-- Known canonical plant cards are safe to expose immediately. The next Saby
-- snapshot replaces this minimal marker with the complete hierarchy for every
-- catalogue item, including still-unlinked cards.
UPDATE saby_nomenclature nomenclature
SET section_path = ARRAY['Комнатные растения']::TEXT[]
WHERE cardinality(nomenclature.section_path) = 0
  AND EXISTS (
    SELECT 1
    FROM products product
    JOIN product_variants variant ON variant.product_id = product.id
    WHERE product.catalog_section = 'plants'
      AND (product.saby_id = nomenclature.saby_id OR variant.saby_id = nomenclature.saby_id)
  );
