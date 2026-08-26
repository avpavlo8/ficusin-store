-- Stage 1 catalogue attribute contract. This migration changes dictionary and
-- category metadata only: no product or variant value is backfilled, removed,
-- or rewritten. Re-running the statements is safe.

INSERT INTO attribute_options(attribute_id,code,label,sort_order,is_active)
SELECT definition.id,seed.code,seed.label,seed.sort_order,TRUE
FROM attribute_definitions definition
JOIN (VALUES
 ('pot_type','cachepot','Кашпо',10),
 ('pot_type','planting_pot','Горшок',20),
 ('pot_type','planter','Вазон',30),
 ('pot_type','hanging','Подвесное',40),
 ('pot_type','self_watering','С автополивом',50),
 ('soil_type','ready_mix','Готовый грунт',10),
 ('soil_type','component','Компонент',20),
 ('soil_type','drainage','Дренаж',30),
 ('soil_type','mineral_substrate','Минеральный субстрат',40)
) seed(attribute_code,code,label,sort_order) ON seed.attribute_code=definition.code
ON CONFLICT(attribute_id,code) DO UPDATE
SET label=EXCLUDED.label,sort_order=EXCLUDED.sort_order,is_active=TRUE;

-- Stage 1 is a content-readiness target, not a database save gate for the
-- already populated catalogue.
UPDATE category_attributes assignment SET is_required=FALSE
FROM categories category
WHERE assignment.category_id=category.id
  AND category.slug IN ('plants','soil','fertilizer','pots','accessories');

-- Filters remain attached to their merchandise root. This prevents a plant
-- facet from becoming effective for pots or another sibling category.
UPDATE catalog_filters filter SET category_id=category.id
FROM categories category
WHERE (category.slug='plants' AND filter.code IN ('light','watering','care','pot','pets'))
   OR (category.slug='soil' AND filter.code IN ('soil-type','soil-target','soil-volume'))
   OR (category.slug='pots' AND filter.code IN ('pot-type','pot-material','pot-diameter','pot-drainage'))
   OR (category.slug='fertilizer' AND filter.code IN ('fertilizer-form','fertilizer-basis','fertilizer-target'))
   OR (category.slug='accessories' AND filter.code IN ('accessory-type','accessory-material'));
