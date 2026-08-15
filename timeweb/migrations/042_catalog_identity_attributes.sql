-- Stable internal catalogue identity and category-driven attributes.
-- Existing numeric PKs, slugs, order references and Saby columns remain in
-- place for backwards compatibility; integrations use this mapping going
-- forward instead of becoming product identity.
CREATE SEQUENCE IF NOT EXISTS ficusin_sku_seq START WITH 1;

CREATE TABLE IF NOT EXISTS product_external_ids (
  id BIGSERIAL PRIMARY KEY,
  product_id BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  variant_id BIGINT REFERENCES product_variants(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  id_type TEXT NOT NULL,
  external_id TEXT NOT NULL,
  metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (provider, id_type, external_id),
  UNIQUE (product_id, variant_id, provider, id_type)
);
CREATE INDEX IF NOT EXISTS product_external_ids_product_idx ON product_external_ids(product_id);
CREATE INDEX IF NOT EXISTS product_external_ids_variant_idx ON product_external_ids(variant_id);

-- Preserve every identifier that previously occupied sku before replacing it.
INSERT INTO product_external_ids(product_id, variant_id, provider, id_type, external_id)
SELECT product_id, id, 'ficusin', 'legacy_sku', sku
FROM product_variants
WHERE NULLIF(BTRIM(sku), '') IS NOT NULL
ON CONFLICT DO NOTHING;

-- Move the sequence beyond already assigned FIC numbers (safe on retry).
DO $$ DECLARE current_max BIGINT;
BEGIN
  SELECT COALESCE(MAX(SUBSTRING(sku FROM 5)::BIGINT),0) INTO current_max
  FROM product_variants WHERE sku ~ '^FIC-[0-9]{6}$';
  IF current_max=0 THEN PERFORM setval('ficusin_sku_seq',1,FALSE);
  ELSE PERFORM setval('ficusin_sku_seq',current_max,TRUE); END IF;
END $$;
UPDATE product_variants
SET sku = 'FIC-' || LPAD(nextval('ficusin_sku_seq')::TEXT, 6, '0')
WHERE sku !~ '^FIC-[0-9]{6}$';
CREATE UNIQUE INDEX IF NOT EXISTS product_variants_ficusin_sku_uidx ON product_variants(sku);
ALTER TABLE product_variants DROP CONSTRAINT IF EXISTS product_variants_ficusin_sku_format;
ALTER TABLE product_variants ADD CONSTRAINT product_variants_ficusin_sku_format
  CHECK (sku ~ '^FIC-[0-9]{6}$') NOT VALID;
ALTER TABLE product_variants VALIDATE CONSTRAINT product_variants_ficusin_sku_format;

CREATE OR REPLACE FUNCTION prevent_ficusin_sku_change() RETURNS trigger AS $$
BEGIN
  IF OLD.sku IS DISTINCT FROM NEW.sku THEN
    RAISE EXCEPTION 'Ficusin SKU is immutable';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS product_variants_immutable_sku ON product_variants;
CREATE TRIGGER product_variants_immutable_sku BEFORE UPDATE OF sku ON product_variants
FOR EACH ROW EXECUTE FUNCTION prevent_ficusin_sku_change();

-- Backfill integration mapping without removing legacy columns yet.
INSERT INTO product_external_ids(product_id, variant_id, provider, id_type, external_id)
SELECT p.id, NULL, 'saby', 'id', p.saby_id FROM products p WHERE p.saby_id IS NOT NULL
ON CONFLICT DO NOTHING;
INSERT INTO product_external_ids(product_id, variant_id, provider, id_type, external_id)
SELECT pv.product_id, pv.id, 'saby', 'id', pv.saby_id FROM product_variants pv WHERE pv.saby_id IS NOT NULL
ON CONFLICT (provider,id_type,external_id) DO UPDATE SET
 variant_id=EXCLUDED.variant_id, updated_at=CURRENT_TIMESTAMP;
INSERT INTO product_external_ids(product_id, variant_id, provider, id_type, external_id)
SELECT p.id, NULL, 'saby', 'code', n.code FROM products p
JOIN saby_nomenclature n ON n.saby_id=p.saby_id WHERE NULLIF(BTRIM(n.code), '') IS NOT NULL
ON CONFLICT DO NOTHING;
INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id)
SELECT p.id,pv.id,'wildberries','sku',pc.wb_nm_id::TEXT
FROM procurement_product_channels pc JOIN products p ON p.saby_id=pc.saby_id
LEFT JOIN LATERAL (SELECT id FROM product_variants WHERE product_id=p.id ORDER BY is_active DESC,id LIMIT 1) pv ON TRUE
WHERE pc.wb_nm_id IS NOT NULL ON CONFLICT DO NOTHING;
INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id)
SELECT p.id,pv.id,'wildberries','vendor_code',pc.wb_vendor_code
FROM procurement_product_channels pc JOIN products p ON p.saby_id=pc.saby_id
LEFT JOIN LATERAL (SELECT id FROM product_variants WHERE product_id=p.id ORDER BY is_active DESC,id LIMIT 1) pv ON TRUE
WHERE NULLIF(BTRIM(pc.wb_vendor_code),'') IS NOT NULL ON CONFLICT DO NOTHING;
INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id)
SELECT p.id,pv.id,'ozon','offer_id',pc.ozon_offer_id
FROM procurement_product_channels pc JOIN products p ON p.saby_id=pc.saby_id
LEFT JOIN LATERAL (SELECT id FROM product_variants WHERE product_id=p.id ORDER BY is_active DESC,id LIMIT 1) pv ON TRUE
WHERE NULLIF(BTRIM(pc.ozon_offer_id),'') IS NOT NULL ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS attribute_definitions (
  id BIGSERIAL PRIMARY KEY,
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  data_type TEXT NOT NULL CHECK (data_type IN ('text','number','boolean','enum','multi_enum')),
  unit TEXT NOT NULL DEFAULT '',
  options JSONB NOT NULL DEFAULT '[]'::JSONB,
  audience TEXT NOT NULL DEFAULT 'customer' CHECK (audience IN ('customer','technical')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS category_attributes (
  category_id BIGINT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
  attribute_id BIGINT NOT NULL REFERENCES attribute_definitions(id) ON DELETE CASCADE,
  is_required BOOLEAN NOT NULL DEFAULT FALSE,
  is_filterable BOOLEAN NOT NULL DEFAULT FALSE,
  show_on_pdp BOOLEAN NOT NULL DEFAULT TRUE,
  is_badge BOOLEAN NOT NULL DEFAULT FALSE,
  sort_order INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(category_id, attribute_id)
);
CREATE TABLE IF NOT EXISTS product_attribute_values (
  product_id BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  attribute_id BIGINT NOT NULL REFERENCES attribute_definitions(id) ON DELETE CASCADE,
  value JSONB NOT NULL,
  source TEXT NOT NULL DEFAULT 'local' CHECK (source IN ('local','saby','import')),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(product_id, attribute_id)
);

INSERT INTO attribute_definitions(code,name,data_type,unit,options,audience) VALUES
 ('height_cm','Высота','number','см','[]','customer'),
 ('pot_diameter_cm','Диаметр горшка','number','см','[]','customer'),
 ('light_level','Освещение','enum','','["sunny","diffused","low_light"]','customer'),
 ('watering','Полив','enum','','["frequent","moderate","rare"]','customer'),
 ('humidity','Влажность','enum','','["low","medium","high"]','customer'),
 ('care_level','Сложность ухода','enum','','["easy","medium","demanding"]','customer'),
 ('toxicity','Токсичность','enum','','["non_toxic","toxic","unknown"]','customer'),
 ('pet_safety','Безопасность для животных','enum','','["safe","caution","unknown"]','customer'),
 ('placement','Подходящие помещения','multi_enum','','["bathroom","bedroom","office","nursery","living_room","kitchen"]','customer'),
 ('growth_habit','Форма роста','enum','','["upright","bushy","trailing","climbing","rosette"]','customer'),
 ('package_length_cm','Длина упаковки','number','см','[]','technical'),
 ('package_width_cm','Ширина упаковки','number','см','[]','technical'),
 ('package_height_cm','Высота упаковки','number','см','[]','technical'),
 ('package_weight_grams','Вес брутто','number','г','[]','technical')
ON CONFLICT(code) DO UPDATE SET name=EXCLUDED.name, data_type=EXCLUDED.data_type,
 unit=EXCLUDED.unit, options=EXCLUDED.options, audience=EXCLUDED.audience;

WITH RECURSIVE plant_categories AS (
 SELECT id FROM categories WHERE slug='plants'
 UNION ALL SELECT c.id FROM categories c JOIN plant_categories p ON c.parent_id=p.id
)
INSERT INTO category_attributes(category_id,attribute_id,is_required,is_filterable,show_on_pdp,is_badge,sort_order)
SELECT c.id, a.id,
  a.code IN ('height_cm','pot_diameter_cm'),
  a.code IN ('height_cm','pot_diameter_cm','light_level','watering','care_level','pet_safety','placement'),
  a.audience='customer', a.code IN ('light_level','care_level','pet_safety'),
  CASE a.code WHEN 'height_cm' THEN 10 WHEN 'pot_diameter_cm' THEN 20 WHEN 'light_level' THEN 30
    WHEN 'watering' THEN 40 WHEN 'humidity' THEN 50 WHEN 'care_level' THEN 60 WHEN 'toxicity' THEN 70
    WHEN 'pet_safety' THEN 80 WHEN 'placement' THEN 90 ELSE 100 END
FROM plant_categories c CROSS JOIN attribute_definitions a
ON CONFLICT(category_id,attribute_id) DO NOTHING;

-- Seed normalized values from the old columns. Old columns remain readable.
INSERT INTO product_attribute_values(product_id,attribute_id,value)
SELECT p.id,a.id,to_jsonb(v.value) FROM products p
CROSS JOIN LATERAL (VALUES ('light_level',p.light_level),('watering',p.watering),
 ('care_level',p.care_level),('pet_safety',p.pet_safety),('growth_habit',p.growth_habit)) v(code,value)
JOIN attribute_definitions a ON a.code=v.code WHERE NULLIF(BTRIM(v.value),'') IS NOT NULL
ON CONFLICT(product_id,attribute_id) DO NOTHING;

-- Compatibility bridge while older readers still use typed legacy columns.
-- Any write path (admin, import or maintenance SQL) keeps normalized values
-- current, so migration to the generic model can be gradual and reversible.
CREATE OR REPLACE FUNCTION sync_variant_catalog_attributes() RETURNS trigger AS $$
BEGIN
  INSERT INTO product_attribute_values(product_id,attribute_id,value,source,updated_at)
  SELECT NEW.product_id,a.id,to_jsonb(v.value),'local',CURRENT_TIMESTAMP
  FROM (VALUES ('height_cm',NEW.height_cm),('pot_diameter_cm',NEW.pot_diameter_cm),
    ('package_length_cm',NEW.package_length_cm),('package_width_cm',NEW.package_width_cm),
    ('package_height_cm',NEW.package_height_cm),('package_weight_grams',NEW.package_weight_grams)) v(code,value)
  JOIN attribute_definitions a ON a.code=v.code WHERE v.value IS NOT NULL
  ON CONFLICT(product_id,attribute_id) DO UPDATE SET value=EXCLUDED.value,updated_at=CURRENT_TIMESTAMP;
  RETURN NEW;
END $$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS product_variants_sync_attributes ON product_variants;
CREATE TRIGGER product_variants_sync_attributes AFTER INSERT OR UPDATE OF height_cm,pot_diameter_cm,
 package_length_cm,package_width_cm,package_height_cm,package_weight_grams ON product_variants
FOR EACH ROW EXECUTE FUNCTION sync_variant_catalog_attributes();

CREATE OR REPLACE FUNCTION sync_product_catalog_attributes() RETURNS trigger AS $$
BEGIN
  INSERT INTO product_attribute_values(product_id,attribute_id,value,source,updated_at)
  SELECT NEW.id,a.id,CASE WHEN v.code='placement' THEN jsonb_build_array(v.value) ELSE to_jsonb(v.value) END,
    'local',CURRENT_TIMESTAMP
  FROM (VALUES ('light_level',NEW.light_level),('watering',NEW.watering),('care_level',NEW.care_level),
    ('pet_safety',NEW.pet_safety),('placement',NEW.placement),('growth_habit',NEW.growth_habit)) v(code,value)
  JOIN attribute_definitions a ON a.code=v.code WHERE NULLIF(BTRIM(v.value),'') IS NOT NULL
  ON CONFLICT(product_id,attribute_id) DO UPDATE SET value=EXCLUDED.value,updated_at=CURRENT_TIMESTAMP;
  RETURN NEW;
END $$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS products_sync_attributes ON products;
CREATE TRIGGER products_sync_attributes AFTER INSERT OR UPDATE OF light_level,watering,care_level,
 pet_safety,placement,growth_habit ON products FOR EACH ROW EXECUTE FUNCTION sync_product_catalog_attributes();
INSERT INTO product_attribute_values(product_id,attribute_id,value)
SELECT pv.product_id,a.id,to_jsonb(v.value) FROM product_variants pv
CROSS JOIN LATERAL (VALUES ('height_cm',pv.height_cm),('pot_diameter_cm',pv.pot_diameter_cm),
 ('package_length_cm',pv.package_length_cm),('package_width_cm',pv.package_width_cm),
 ('package_height_cm',pv.package_height_cm),('package_weight_grams',pv.package_weight_grams)) v(code,value)
JOIN attribute_definitions a ON a.code=v.code WHERE v.value IS NOT NULL
ON CONFLICT(product_id,attribute_id) DO NOTHING;

ALTER TABLE categories ADD COLUMN IF NOT EXISTS icon TEXT NOT NULL DEFAULT 'leaf';
UPDATE categories SET icon=CASE
 WHEN slug LIKE '%pot%' OR slug LIKE '%kashpo%' THEN 'pot'
 WHEN slug LIKE '%soil%' OR slug LIKE '%grunt%' THEN 'soil'
 WHEN slug LIKE '%fert%' OR slug LIKE '%udob%' THEN 'fertilizer'
 WHEN slug LIKE '%access%' OR slug LIKE '%aksess%' THEN 'tools'
 ELSE 'leaf' END;
