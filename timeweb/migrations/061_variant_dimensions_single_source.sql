-- Physical dimensions of a sellable variant get exactly one home.
--
-- Until now the same facts lived twice: in product_variants columns and in
-- variant_attribute_values. Migration 044 kept them in sync with the
-- product_variants_sync_attributes trigger, migration 056 dropped that trigger
-- and put nothing in its place. From that moment the legacy product form wrote
-- only columns, the new SKU editor wrote only attributes, and delivery, the
-- product page and marketplace logistics all read columns. Editing a parcel
-- size in PIM did not change the delivery price; editing height in the legacy
-- form did not change the storefront.
--
-- PIM wins. The columns stay for one release so a rollback keeps working, but
-- nothing reads them after this migration.

-- 0. The attribute registry never had updated_at, yet the admin code writes it
-- on every edit and on archiving. Both statements failed with "column
-- updated_at does not exist", so the registry could not be edited at all.
ALTER TABLE attribute_definitions
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;

-- 1. Nothing may be lost. Copy every value that exists only in a column.
INSERT INTO variant_attribute_values(variant_id, attribute_id, value, source, updated_at)
SELECT variant.id, definition.id, to_jsonb(source.value), 'import', CURRENT_TIMESTAMP
FROM product_variants variant
CROSS JOIN LATERAL (VALUES
  ('height_cm', variant.height_cm),
  ('pot_diameter_cm', variant.pot_diameter_cm),
  ('package_length_cm', variant.package_length_cm),
  ('package_width_cm', variant.package_width_cm),
  ('package_height_cm', variant.package_height_cm),
  ('package_weight_grams', variant.package_weight_grams)
) source(code, value)
JOIN attribute_definitions definition ON definition.code = source.code
WHERE source.value IS NOT NULL
ON CONFLICT(variant_id, attribute_id) DO NOTHING;

-- 2. A parcel size is a property of a physical object, not of a catalogue
-- section. Delivery has to price accessories and soil as well as plants, so
-- these definitions stop depending on a category assignment.
UPDATE attribute_definitions
SET is_global = TRUE, is_active = TRUE, updated_at = CURRENT_TIMESTAMP
WHERE code IN (
  'height_cm','pot_diameter_cm',
  'package_length_cm','package_width_cm','package_height_cm','package_weight_grams'
);

-- 3. One readable way to get a numeric PIM value. A malformed value returns
-- NULL instead of aborting the caller's query: a typo in one SKU must not take
-- the whole storefront or the whole delivery quote down.
CREATE OR REPLACE FUNCTION variant_numeric_attribute(target_variant BIGINT, attribute_code TEXT)
RETURNS NUMERIC
LANGUAGE sql
STABLE
AS $$
  SELECT CASE
    WHEN jsonb_typeof(value.value) = 'number' THEN (value.value #>> '{}')::NUMERIC
    WHEN value.value #>> '{}' ~ '^-?[0-9]+(\.[0-9]+)?$' THEN (value.value #>> '{}')::NUMERIC
    ELSE NULL
  END
  FROM variant_attribute_values value
  JOIN attribute_definitions definition ON definition.id = value.attribute_id
  WHERE value.variant_id = target_variant AND definition.code = attribute_code
  LIMIT 1
$$;

CREATE INDEX IF NOT EXISTS variant_attribute_values_variant_idx
  ON variant_attribute_values(variant_id, attribute_id);

-- 4. Prove the bridge on the data we just migrated: every variant that had a
-- parcel weight in a column must now answer with the same number through the
-- attribute store. Fail here rather than on a customer's delivery quote.
DO $$
DECLARE
  mismatched BIGINT;
BEGIN
  SELECT COUNT(*) INTO mismatched
  FROM product_variants variant
  WHERE variant.package_weight_grams IS NOT NULL
    AND COALESCE(variant_numeric_attribute(variant.id, 'package_weight_grams'), -1)
        <> variant.package_weight_grams::NUMERIC;
  IF mismatched > 0 THEN
    RAISE EXCEPTION 'PIM lost % parcel weights during the dimension migration', mismatched;
  END IF;
END;
$$;

COMMENT ON COLUMN product_variants.package_weight_grams IS
  'Deprecated since migration 061. PIM (variant_attribute_values) is authoritative; this column is kept for one release only.';
COMMENT ON COLUMN product_variants.height_cm IS
  'Deprecated since migration 061. PIM (variant_attribute_values) is authoritative; this column is kept for one release only.';
