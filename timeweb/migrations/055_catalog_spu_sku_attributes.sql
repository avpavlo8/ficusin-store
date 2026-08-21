-- Catalogue v2: clean product-card (SPU) + sellable-variant (SKU) identity.
-- Public product codes and SKUs are simple decimal numbers. External provider
-- identifiers remain mappings only and never become catalogue identity.

DROP TRIGGER IF EXISTS product_variants_immutable_sku ON product_variants;
DROP FUNCTION IF EXISTS prevent_ficusin_sku_change();
ALTER TABLE product_variants DROP CONSTRAINT IF EXISTS product_variants_ficusin_sku_format;
DROP INDEX IF EXISTS product_variants_ficusin_sku_uidx;

CREATE TEMP TABLE tmp_product_identity AS
SELECT id AS product_id, slug AS old_code,
       ROW_NUMBER() OVER (ORDER BY id)::TEXT AS new_code
FROM products;

CREATE TEMP TABLE tmp_variant_identity AS
SELECT id AS variant_id, product_id, sku AS old_sku,
       ROW_NUMBER() OVER (ORDER BY id)::TEXT AS new_sku
FROM product_variants;

-- Old carts were keyed by the public product slug. From this release a cart
-- contains SKUs, so two sizes of the same product are independent lines.
UPDATE customer_carts cart
SET items = COALESCE((
  SELECT jsonb_object_agg(primary_variant.new_sku, entry.value)
  FROM jsonb_each(cart.items) entry
  JOIN tmp_product_identity product_map ON product_map.old_code = entry.key
  JOIN LATERAL (
    SELECT variant_map.new_sku
    FROM tmp_variant_identity variant_map
    WHERE variant_map.product_id = product_map.product_id
    ORDER BY variant_map.variant_id
    LIMIT 1
  ) primary_variant ON TRUE
), '{}'::JSONB);

UPDATE guest_carts cart
SET items = COALESCE((
  SELECT jsonb_object_agg(primary_variant.new_sku, entry.value)
  FROM jsonb_each(cart.items) entry
  JOIN tmp_product_identity product_map ON product_map.old_code = entry.key
  JOIN LATERAL (
    SELECT variant_map.new_sku
    FROM tmp_variant_identity variant_map
    WHERE variant_map.product_id = product_map.product_id
    ORDER BY variant_map.variant_id
    LIMIT 1
  ) primary_variant ON TRUE
), '{}'::JSONB);

-- Historical order rows already carry variant_id for current orders. Older
-- rows are attached to the first variant of their old product before the
-- public identifier is replaced. product_id stays TEXT for compatibility but
-- from now on its value is the purchased SKU, not a product slug.
UPDATE order_items item
SET variant_id = primary_variant.variant_id
FROM tmp_product_identity product_map
JOIN LATERAL (
  SELECT variant_map.variant_id
  FROM tmp_variant_identity variant_map
  WHERE variant_map.product_id = product_map.product_id
  ORDER BY variant_map.variant_id
  LIMIT 1
) primary_variant ON TRUE
WHERE item.variant_id IS NULL AND item.product_id = product_map.old_code;

UPDATE order_items item
SET product_id = variant_map.new_sku
FROM tmp_variant_identity variant_map
WHERE item.variant_id = variant_map.variant_id;

-- Remove the old public identity values themselves.
UPDATE products product
SET slug = product_map.new_code
FROM tmp_product_identity product_map
WHERE product.id = product_map.product_id;

UPDATE product_variants variant
SET sku = variant_map.new_sku
FROM tmp_variant_identity variant_map
WHERE variant.id = variant_map.variant_id;

-- Old Ficusin SKU aliases are deliberately not preserved. Saby/WB/Ozon IDs
-- remain external mappings, because those are integration facts, not SKUs.
DELETE FROM product_external_ids WHERE provider = 'ficusin';

DROP SEQUENCE IF EXISTS ficusin_sku_seq;
CREATE SEQUENCE ficusin_sku_seq;
SELECT setval('ficusin_sku_seq', COALESCE((SELECT MAX(sku::BIGINT) FROM product_variants), 0) + 1, FALSE);
ALTER TABLE product_variants ALTER COLUMN sku SET DEFAULT nextval('ficusin_sku_seq')::TEXT;
ALTER TABLE product_variants ADD CONSTRAINT product_variants_sku_numeric
  CHECK (sku ~ '^[1-9][0-9]*$');
CREATE UNIQUE INDEX IF NOT EXISTS product_variants_sku_uidx ON product_variants(sku);

CREATE SEQUENCE IF NOT EXISTS ficusin_product_code_seq;
SELECT setval('ficusin_product_code_seq', COALESCE((SELECT MAX(slug::BIGINT) FROM products), 0) + 1, FALSE);
ALTER TABLE products ALTER COLUMN slug SET DEFAULT nextval('ficusin_product_code_seq')::TEXT;
ALTER TABLE products ADD CONSTRAINT products_code_numeric
  CHECK (slug ~ '^[1-9][0-9]*$');

CREATE OR REPLACE FUNCTION prevent_catalogue_identity_change() RETURNS trigger AS $$
BEGIN
  IF TG_TABLE_NAME = 'product_variants' AND OLD.sku IS DISTINCT FROM NEW.sku THEN
    RAISE EXCEPTION 'Ficusin SKU is immutable';
  END IF;
  IF TG_TABLE_NAME = 'products' AND OLD.slug IS DISTINCT FROM NEW.slug THEN
    RAISE EXCEPTION 'Ficusin product code is immutable';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS product_variants_immutable_sku ON product_variants;
CREATE TRIGGER product_variants_immutable_sku
BEFORE UPDATE OF sku ON product_variants
FOR EACH ROW EXECUTE FUNCTION prevent_catalogue_identity_change();
DROP TRIGGER IF EXISTS products_immutable_code ON products;
CREATE TRIGGER products_immutable_code
BEFORE UPDATE OF slug ON products
FOR EACH ROW EXECUTE FUNCTION prevent_catalogue_identity_change();

-- Attribute definitions know whether a value is shared by the whole product
-- card or belongs to one sellable SKU. This prevents size/package values from
-- one variant overwriting another variant of the same product.
ALTER TABLE attribute_definitions
  ADD COLUMN IF NOT EXISTS value_scope TEXT NOT NULL DEFAULT 'product',
  ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE attribute_definitions DROP CONSTRAINT IF EXISTS attribute_definitions_value_scope_check;
ALTER TABLE attribute_definitions ADD CONSTRAINT attribute_definitions_value_scope_check
  CHECK (value_scope IN ('product','variant'));

UPDATE attribute_definitions
SET value_scope = 'variant'
WHERE code IN ('height_cm','pot_diameter_cm','package_length_cm','package_width_cm','package_height_cm','package_weight_grams');

CREATE TABLE IF NOT EXISTS attribute_options (
  id BIGSERIAL PRIMARY KEY,
  attribute_id BIGINT NOT NULL REFERENCES attribute_definitions(id) ON DELETE CASCADE,
  code TEXT NOT NULL,
  label TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  UNIQUE(attribute_id, code)
);
CREATE INDEX IF NOT EXISTS attribute_options_attribute_idx
  ON attribute_options(attribute_id, sort_order, id);

-- Normalize the old JSON option lists. They remain readable during this
-- deployment but the owner UI and validation use attribute_options.
INSERT INTO attribute_options(attribute_id, code, label, sort_order)
SELECT definition.id, option.value,
       CASE option.value
         WHEN 'sunny' THEN 'Яркий свет' WHEN 'diffused' THEN 'Рассеянный свет' WHEN 'low_light' THEN 'Полутень'
         WHEN 'frequent' THEN 'Частый' WHEN 'moderate' THEN 'Умеренный' WHEN 'rare' THEN 'Редкий'
         WHEN 'low' THEN 'Низкая' WHEN 'medium' THEN 'Средняя' WHEN 'high' THEN 'Высокая'
         WHEN 'easy' THEN 'Лёгкий' WHEN 'demanding' THEN 'Требовательный'
         WHEN 'non_toxic' THEN 'Нетоксично' WHEN 'toxic' THEN 'Токсично' WHEN 'unknown' THEN 'Не проверено'
         WHEN 'safe' THEN 'Безопасно' WHEN 'caution' THEN 'С осторожностью'
         WHEN 'bathroom' THEN 'Ванная' WHEN 'bedroom' THEN 'Спальня' WHEN 'office' THEN 'Офис'
         WHEN 'nursery' THEN 'Детская' WHEN 'living_room' THEN 'Гостиная' WHEN 'kitchen' THEN 'Кухня'
         WHEN 'upright' THEN 'Вертикальная' WHEN 'bushy' THEN 'Кустовая' WHEN 'trailing' THEN 'Ампельная'
         WHEN 'climbing' THEN 'Вьющаяся' WHEN 'rosette' THEN 'Розетка'
         ELSE INITCAP(REPLACE(option.value, '_', ' '))
       END,
       option.ordinality::INTEGER * 10
FROM attribute_definitions definition
CROSS JOIN LATERAL jsonb_array_elements_text(definition.options) WITH ORDINALITY option(value, ordinality)
ON CONFLICT(attribute_id, code) DO NOTHING;

CREATE TABLE IF NOT EXISTS variant_attribute_values (
  variant_id BIGINT NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
  attribute_id BIGINT NOT NULL REFERENCES attribute_definitions(id) ON DELETE CASCADE,
  value JSONB NOT NULL,
  source TEXT NOT NULL DEFAULT 'local' CHECK (source IN ('local','saby','import')),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(variant_id, attribute_id)
);
CREATE INDEX IF NOT EXISTS variant_attribute_values_attribute_idx
  ON variant_attribute_values(attribute_id, variant_id);

INSERT INTO variant_attribute_values(variant_id, attribute_id, value)
SELECT variant.id, definition.id, to_jsonb(source.value)
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
ON CONFLICT(variant_id, attribute_id) DO UPDATE
SET value = EXCLUDED.value, updated_at = CURRENT_TIMESTAMP;

DELETE FROM product_attribute_values value
USING attribute_definitions definition
WHERE value.attribute_id = definition.id AND definition.value_scope = 'variant';

DROP TRIGGER IF EXISTS product_variants_sync_attributes ON product_variants;
DROP FUNCTION IF EXISTS sync_variant_catalog_attributes();
CREATE OR REPLACE FUNCTION sync_variant_catalog_attributes() RETURNS trigger AS $$
BEGIN
  INSERT INTO variant_attribute_values(variant_id, attribute_id, value, source, updated_at)
  SELECT NEW.id, definition.id, to_jsonb(source.value), 'local', CURRENT_TIMESTAMP
  FROM (VALUES
    ('height_cm',NEW.height_cm),('pot_diameter_cm',NEW.pot_diameter_cm),
    ('package_length_cm',NEW.package_length_cm),('package_width_cm',NEW.package_width_cm),
    ('package_height_cm',NEW.package_height_cm),('package_weight_grams',NEW.package_weight_grams)
  ) source(code,value)
  JOIN attribute_definitions definition ON definition.code=source.code
  WHERE source.value IS NOT NULL
  ON CONFLICT(variant_id,attribute_id) DO UPDATE
  SET value=EXCLUDED.value, updated_at=CURRENT_TIMESTAMP;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER product_variants_sync_attributes
AFTER INSERT OR UPDATE OF height_cm,pot_diameter_cm,package_length_cm,package_width_cm,package_height_cm,package_weight_grams
ON product_variants FOR EACH ROW EXECUTE FUNCTION sync_variant_catalog_attributes();

-- PDP placement is a property of the category assignment, not the global
-- attribute definition. A child category can override an inherited setting.
ALTER TABLE category_attributes
  ADD COLUMN IF NOT EXISTS show_in_summary BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS summary_position INTEGER,
  ADD COLUMN IF NOT EXISTS show_in_characteristics BOOLEAN NOT NULL DEFAULT TRUE,
  ADD COLUMN IF NOT EXISTS is_excluded BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE category_attributes assignment
SET show_in_summary = definition.code IN ('light_level','watering','height_cm','pot_diameter_cm','care_level','pet_safety','humidity'),
    summary_position = CASE definition.code
      WHEN 'light_level' THEN 1 WHEN 'watering' THEN 2 WHEN 'height_cm' THEN 3
      WHEN 'pot_diameter_cm' THEN 4 WHEN 'care_level' THEN 5 WHEN 'pet_safety' THEN 6
      WHEN 'humidity' THEN 7 ELSE NULL END,
    show_in_characteristics = definition.audience = 'customer'
FROM attribute_definitions definition
WHERE definition.id = assignment.attribute_id;

-- Useful long-lived definitions that are not tied to a hard-coded React form.
INSERT INTO attribute_definitions(code,name,data_type,unit,options,audience,value_scope) VALUES
 ('plant_type','Тип растения','enum','','[]','customer','product'),
 ('net_weight_g','Масса нетто','number','г','[]','customer','variant'),
 ('volume_ml','Объём','number','мл','[]','customer','variant'),
 ('target_plant','Для каких растений','multi_enum','','[]','customer','product')
ON CONFLICT(code) DO UPDATE SET name=EXCLUDED.name,data_type=EXCLUDED.data_type,unit=EXCLUDED.unit,
 audience=EXCLUDED.audience,value_scope=EXCLUDED.value_scope;

INSERT INTO attribute_options(attribute_id,code,label,sort_order)
SELECT definition.id, option.code, option.label, option.sort_order
FROM attribute_definitions definition
JOIN (VALUES
 ('palm','Пальма',10),('cactus','Кактус',20),('bonsai','Бонсай',30),('succulent','Суккулент',40),
 ('fern','Папоротник',50),('orchid','Орхидея',60),('flowering','Цветущее',70),('decorative_leaf','Декоративно-лиственное',80)
) option(code,label,sort_order) ON TRUE
WHERE definition.code='plant_type'
ON CONFLICT(attribute_id,code) DO UPDATE SET label=EXCLUDED.label,sort_order=EXCLUDED.sort_order;

WITH RECURSIVE plant_tree AS (
  SELECT id FROM categories WHERE slug='plants'
  UNION ALL SELECT category.id FROM categories category JOIN plant_tree parent ON category.parent_id=parent.id
)
INSERT INTO category_attributes(category_id,attribute_id,is_required,is_filterable,show_on_pdp,is_badge,sort_order,show_in_summary,summary_position,show_in_characteristics)
SELECT category.id, definition.id, FALSE, TRUE, TRUE, FALSE, 15, FALSE, NULL, TRUE
FROM plant_tree category CROSS JOIN attribute_definitions definition
WHERE definition.code='plant_type'
ON CONFLICT(category_id,attribute_id) DO NOTHING;

-- Filter configuration controls which attributes become storefront controls.
-- Values themselves are always derived from filled published SKU/product data.
CREATE TABLE IF NOT EXISTS catalog_filters (
  id BIGSERIAL PRIMARY KEY,
  code TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  attribute_id BIGINT NOT NULL REFERENCES attribute_definitions(id) ON DELETE RESTRICT,
  category_id BIGINT REFERENCES categories(id) ON DELETE CASCADE,
  display_mode TEXT NOT NULL DEFAULT 'select' CHECK(display_mode IN ('select','chips','range')),
  sort_order INTEGER NOT NULL DEFAULT 0,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO catalog_filters(code,title,attribute_id,display_mode,sort_order)
SELECT seed.code, seed.title, definition.id, seed.mode, seed.sort_order
FROM (VALUES
 ('light','Освещённость','light_level','select',10),
 ('watering','Полив','watering','select',20),
 ('care','Уход','care_level','select',30),
 ('pot','Размер горшка','pot_diameter_cm','select',40),
 ('pets','Для питомцев','pet_safety','select',50),
 ('plant-type','Тип растения','plant_type','select',60)
) seed(code,title,attribute_code,mode,sort_order)
JOIN attribute_definitions definition ON definition.code=seed.attribute_code
ON CONFLICT(code) DO UPDATE SET title=EXCLUDED.title,attribute_id=EXCLUDED.attribute_id,
 display_mode=EXCLUDED.display_mode,sort_order=EXCLUDED.sort_order;

-- Reviews are shown on the product card but remember exactly which SKU was
-- purchased. Existing reviews are backfilled from their order line. PostgreSQL
-- does not allow an UPDATE target alias to be referenced from a LATERAL item in
-- the UPDATE's FROM clause, so resolve the match first and update by review id.
ALTER TABLE product_reviews ADD COLUMN IF NOT EXISTS variant_id BIGINT REFERENCES product_variants(id) ON DELETE SET NULL;
WITH review_variant AS (
  SELECT review.id AS review_id,
         (
           SELECT item.variant_id
           FROM order_items item
           JOIN product_variants variant ON variant.id=item.variant_id
           WHERE item.order_id=review.order_id AND variant.product_id=review.product_id
           ORDER BY item.id
           LIMIT 1
         ) AS variant_id
  FROM product_reviews review
  WHERE review.variant_id IS NULL
)
UPDATE product_reviews review
SET variant_id = match.variant_id
FROM review_variant match
WHERE review.id = match.review_id AND match.variant_id IS NOT NULL;
