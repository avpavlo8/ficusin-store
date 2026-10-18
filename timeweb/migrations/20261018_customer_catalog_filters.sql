-- Add buyer-facing filters without guessing or backfilling plant facts.
-- Growing country belongs to a supplied variant, not a species' native range.
INSERT INTO attribute_definitions(code,name,data_type,unit,audience,value_scope,is_global,is_active,description)
VALUES ('growing_country','Страна выращивания','text','','customer','variant',FALSE,TRUE,
  'Подтверждённая страна выращивания данного варианта. Не страна поставщика и не природный ареал вида.')
ON CONFLICT(code) DO NOTHING;

-- Reuse the existing canonical care attribute and its machine values.
UPDATE attribute_definitions SET name='Сложность ухода' WHERE code='care_level';
UPDATE attribute_options option SET label=seed.label
FROM attribute_definitions definition,
  (VALUES ('easy','Лёгкий уход'),('medium','Средний уход'),('demanding','Сложный уход')) seed(code,label)
WHERE option.attribute_id=definition.id AND definition.code='care_level' AND option.code=seed.code;
UPDATE catalog_filters filter SET title='Сложность ухода',display_mode='chips'
FROM attribute_definitions definition
WHERE filter.attribute_id=definition.id AND definition.code='care_level';

INSERT INTO category_attributes(category_id,attribute_id,is_required,is_filterable,show_on_pdp,is_badge,sort_order,show_in_summary,show_in_characteristics,is_excluded)
SELECT category.id,definition.id,FALSE,TRUE,TRUE,FALSE,110,FALSE,TRUE,FALSE
FROM categories category CROSS JOIN attribute_definitions definition
WHERE category.slug='plants' AND definition.code='growing_country'
ON CONFLICT(category_id,attribute_id) DO NOTHING;

-- Existing child assignments otherwise override the root's flowering setting.
WITH RECURSIVE plant_categories AS (
  SELECT id FROM categories WHERE slug='plants'
  UNION ALL
  SELECT category.id FROM categories category JOIN plant_categories parent ON category.parent_id=parent.id
)
UPDATE category_attributes assignment SET is_filterable=TRUE
FROM attribute_definitions definition
WHERE assignment.attribute_id=definition.id AND definition.code='flowering'
  AND assignment.category_id IN (SELECT id FROM plant_categories);

INSERT INTO catalog_filters(code,title,attribute_id,category_id,display_mode,sort_order,is_active)
SELECT seed.code,seed.title,definition.id,category.id,seed.mode,seed.sort_order,TRUE
FROM (VALUES
  ('plant-height','Высота растения','height_cm','range',5),
  ('plant-flowering','Цветение','flowering','chips',35),
  ('growing-country','Страна выращивания','growing_country','select',70)
) seed(code,title,attribute_code,mode,sort_order)
JOIN attribute_definitions definition ON definition.code=seed.attribute_code
JOIN categories category ON category.slug='plants'
ON CONFLICT(code) DO NOTHING;
