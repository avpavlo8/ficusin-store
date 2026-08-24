-- Recovery/finalization after 071: keep the already applied merchandising
-- merge intact and expose every objective dimension through SKU attributes.

INSERT INTO variant_attribute_values(variant_id,attribute_id,value,source,updated_at)
SELECT variant.id,definition.id,to_jsonb(CASE definition.code
  WHEN 'height_cm' THEN variant.height_cm
  WHEN 'pot_diameter_cm' THEN variant.pot_diameter_cm END),
  'import',CURRENT_TIMESTAMP
FROM product_variants variant
JOIN products product ON product.id=variant.product_id AND product.catalog_section='plants'
JOIN attribute_definitions definition ON
  (definition.code='height_cm' AND variant.height_cm IS NOT NULL) OR
  (definition.code='pot_diameter_cm' AND variant.pot_diameter_cm IS NOT NULL)
ON CONFLICT(variant_id,attribute_id) DO NOTHING;

-- A customer must see the sellable difference before choosing a SKU.
WITH measures AS (
 SELECT value.variant_id,definition.unit,(value.value#>>'{}')::numeric amount,
        row_number() OVER (PARTITION BY value.variant_id ORDER BY
          CASE definition.code WHEN 'volume_l' THEN 1 WHEN 'volume_ml' THEN 2 WHEN 'net_weight_g' THEN 3 ELSE 4 END) position
 FROM variant_attribute_values value JOIN attribute_definitions definition ON definition.id=value.attribute_id
 WHERE definition.code IN ('volume_l','volume_ml','net_weight_g')
)
UPDATE product_variants variant
SET label=trim(to_char(measure.amount,'FM999999990D99'),'.')||' '||measure.unit,
    updated_at=CURRENT_TIMESTAMP
FROM measures measure
WHERE measure.variant_id=variant.id AND measure.position=1
  AND variant.label IN ('Основной вариант','Основной размер');

-- Supplier cards sometimes carry several distinct SKUs but no usable size in
-- their text. Do not show identical controls: number those variants until the
-- manager records the physical dimensions.
WITH numbered AS (
 SELECT variant.id,row_number() OVER(PARTITION BY variant.product_id ORDER BY variant.id) position,
        count(*) OVER(PARTITION BY variant.product_id) total
 FROM product_variants variant WHERE variant.is_active<>0 AND variant.archived_at IS NULL
)
UPDATE product_variants variant
SET label='Вариант '||numbered.position,updated_at=CURRENT_TIMESTAMP
FROM numbered
WHERE numbered.id=variant.id AND numbered.total>1
  AND variant.label IN ('Основной вариант','Основной размер');

-- One remaining pot title uses technical supplier notation. Preserve the
-- dimensions in the SKU and keep the card name readable.
WITH target AS (
 SELECT product.id product_id,variant.id variant_id
 FROM products product JOIN product_variants variant ON variant.product_id=product.id
 WHERE product.product_code=603 ORDER BY variant.id LIMIT 1
), values(code,value) AS (VALUES ('product_diameter_cm',17::numeric),('product_height_cm',15.5::numeric))
INSERT INTO variant_attribute_values(variant_id,attribute_id,value,source,updated_at)
SELECT target.variant_id,definition.id,to_jsonb(values.value),'import',CURRENT_TIMESTAMP
FROM target CROSS JOIN values JOIN attribute_definitions definition ON definition.code=values.code
ON CONFLICT(variant_id,attribute_id) DO NOTHING;
UPDATE products SET name='Кашпо «Аркада» со вкладкой, цвет «Серый муссон»',
 search_text='Кашпо Арка серый муссон со вкладкой',updated_at=CURRENT_TIMESTAMP
WHERE product_code=603;

DO $validate$
BEGIN
 IF EXISTS (
  SELECT 1 FROM products product JOIN product_variants variant ON variant.product_id=product.id
  WHERE product.catalog_section='plants' AND variant.pot_diameter_cm IS NOT NULL
    AND NOT EXISTS(SELECT 1 FROM variant_attribute_values value JOIN attribute_definitions definition ON definition.id=value.attribute_id
                   WHERE value.variant_id=variant.id AND definition.code='pot_diameter_cm')
 ) THEN RAISE EXCEPTION 'plant pot diameter was not exposed as a SKU attribute'; END IF;
 IF EXISTS (
  SELECT 1 FROM product_variants variant
  WHERE variant.label IN ('Основной вариант','Основной размер')
    AND (SELECT count(*) FROM product_variants sibling WHERE sibling.product_id=variant.product_id AND sibling.is_active<>0 AND sibling.archived_at IS NULL)>1
 ) THEN RAISE EXCEPTION 'multi-SKU product still has an ambiguous variant label'; END IF;
END;
$validate$;
