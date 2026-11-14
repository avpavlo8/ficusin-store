-- Migration061 made PIM authoritative; these columns and override arrays were
-- retained for a single rollback release. Preserve every original value now.
LOCK TABLE products,product_variants,attribute_definitions,variant_attribute_values
 IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(
  SELECT 1 FROM product_variants v CROSS JOIN LATERAL(VALUES
   ('height_cm',v.height_cm),('pot_diameter_cm',v.pot_diameter_cm),
   ('package_length_cm',v.package_length_cm),('package_width_cm',v.package_width_cm),
   ('package_height_cm',v.package_height_cm),('package_weight_grams',v.package_weight_grams)
  ) legacy(code,value)
  LEFT JOIN attribute_definitions d ON d.code=legacy.code
  WHERE legacy.value IS NOT NULL AND (d.id IS NULL OR d.data_type<>'number' OR d.value_scope<>'variant')
 ) THEN RAISE EXCEPTION 'Typed dimensions require exact numeric variant attribute definitions before retirement'; END IF;
END $$;
CREATE SCHEMA IF NOT EXISTS legacy_archive;
CREATE TABLE legacy_archive.product_retired_overrides AS
 SELECT id AS product_id,override_fields,created_at,updated_at,CURRENT_TIMESTAMP AS archived_at FROM products;
ALTER TABLE legacy_archive.product_retired_overrides ADD PRIMARY KEY(product_id);
CREATE TABLE legacy_archive.variant_retired_dimensions AS
 SELECT id AS variant_id,product_id,height_cm,pot_diameter_cm,package_length_cm,
  package_width_cm,package_height_cm,package_weight_grams,override_fields,
  created_at,updated_at,CURRENT_TIMESTAMP AS archived_at FROM product_variants;
ALTER TABLE legacy_archive.variant_retired_dimensions ADD PRIMARY KEY(variant_id);

-- Existing rows, including explicit JSON null, strings and manual values, are
-- authoritative. Only an absent row may receive its non-null legacy value.
-- The existing source CHECK must accept 'import'; otherwise this insert fails
-- atomically and no source column is dropped.
INSERT INTO variant_attribute_values(variant_id,attribute_id,value,source,updated_at)
 SELECT v.id,d.id,to_jsonb(legacy.value),'import',CURRENT_TIMESTAMP
 FROM product_variants v CROSS JOIN LATERAL(VALUES
  ('height_cm',v.height_cm),('pot_diameter_cm',v.pot_diameter_cm),
  ('package_length_cm',v.package_length_cm),('package_width_cm',v.package_width_cm),
  ('package_height_cm',v.package_height_cm),('package_weight_grams',v.package_weight_grams)
 ) legacy(code,value)
 JOIN attribute_definitions d ON d.code=legacy.code AND d.data_type='number' AND d.value_scope='variant'
 WHERE legacy.value IS NOT NULL
 ON CONFLICT(variant_id,attribute_id) DO NOTHING;

-- No CASCADE: unexpected dependent objects block deployment. Function bodies
-- are audited separately. Site price, inventory and PIM values stay intact.
ALTER TABLE products DROP COLUMN override_fields;
ALTER TABLE product_variants DROP COLUMN height_cm,DROP COLUMN pot_diameter_cm,
 DROP COLUMN package_length_cm,DROP COLUMN package_width_cm,DROP COLUMN package_height_cm,
 DROP COLUMN package_weight_grams,DROP COLUMN override_fields;
