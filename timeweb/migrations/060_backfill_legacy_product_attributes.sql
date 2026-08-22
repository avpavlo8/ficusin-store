-- Recovery for catalogue rows that predate the generic PIM value store.
-- 044 copied most legacy columns, but omitted placement (and two other
-- product-scoped attributes).  The compatibility trigger only helps after a
-- later edit, so untouched production rows were invisible to dynamic
-- collections even though the public catalogue still exposed their values.

ALTER TABLE products ADD COLUMN IF NOT EXISTS humidity TEXT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS toxicity TEXT;

INSERT INTO attribute_definitions(
  code,name,data_type,unit,audience,value_scope,is_active,is_global,description
) VALUES (
  'height_class','Класс высоты','enum','','customer','product',TRUE,TRUE,
  'Укрупнённая высота карточки товара: low, medium или high'
)
ON CONFLICT(code) DO UPDATE SET
  name=EXCLUDED.name,
  data_type=EXCLUDED.data_type,
  audience=EXCLUDED.audience,
  value_scope=EXCLUDED.value_scope,
  is_active=TRUE,
  is_global=TRUE,
  description=EXCLUDED.description;

INSERT INTO attribute_options(attribute_id,code,label,sort_order,is_active)
SELECT definition.id, option.code, option.label, option.sort_order, TRUE
FROM attribute_definitions definition
CROSS JOIN (VALUES
  ('low','Компактное',10),
  ('medium','Среднее',20),
  ('high','Высокое',30)
) option(code,label,sort_order)
WHERE definition.code='height_class'
ON CONFLICT(attribute_id,code) DO UPDATE SET
  label=EXCLUDED.label,sort_order=EXCLUDED.sort_order,is_active=TRUE;

-- Never overwrite a value already curated in PIM.  This is strictly a
-- one-time recovery of missing normalized values from still-readable legacy
-- columns.
INSERT INTO product_attribute_values(product_id,attribute_id,value,source)
SELECT product.id,definition.id,
  CASE
    WHEN source.code='placement' THEN jsonb_build_array(source.value)
    ELSE to_jsonb(source.value)
  END,
  'local'
FROM products product
CROSS JOIN LATERAL (VALUES
  ('placement',product.placement),
  ('humidity',product.humidity),
  ('toxicity',product.toxicity),
  ('height_class',product.height_class)
) source(code,value)
JOIN attribute_definitions definition ON definition.code=source.code
WHERE NULLIF(BTRIM(source.value),'') IS NOT NULL
ON CONFLICT(product_id,attribute_id) DO NOTHING;

-- The old visual presets were defined in terms of height_class.  Keep that
-- exact business meaning instead of approximating it with a numeric SKU
-- height, which may be absent or differ between variants.
UPDATE collections
SET rules='[{"attribute":"height_class","operator":"eq","value":"high"}]'::jsonb,
    updated_at=CURRENT_TIMESTAMP
WHERE slug='tall' AND mode='dynamic';

UPDATE collections
SET rules='[{"attribute":"height_class","operator":"eq","value":"low"}]'::jsonb,
    updated_at=CURRENT_TIMESTAMP
WHERE slug='compact' AND mode='dynamic';
