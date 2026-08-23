-- Category-specific commerce attributes. Historical values and definitions are
-- deliberately retained: this migration changes the effective category schema,
-- not order/catalogue history.
INSERT INTO attribute_definitions(code,name,data_type,unit,audience,value_scope,is_global,is_active,description) VALUES
 ('flowering','Цветение','boolean','','customer','product',FALSE,TRUE,'Цветёт ли растение в комнатных условиях'),
 ('soil_type','Тип грунта','enum','','customer','product',FALSE,TRUE,'Готовый грунт, компонент или дренаж'),
 ('volume_l','Объём','number','л','customer','variant',FALSE,TRUE,'Полезный объём товара'),
 ('composition','Состав','text','','customer','product',FALSE,TRUE,'Основные компоненты'),
 ('ph_min','pH, от','number','','customer','product',FALSE,TRUE,'Нижняя граница кислотности'),
 ('ph_max','pH, до','number','','customer','product',FALSE,TRUE,'Верхняя граница кислотности'),
 ('contains_fertilizer','Содержит удобрение','boolean','','customer','product',FALSE,TRUE,'Есть стартовая подкормка'),
 ('pot_type','Тип кашпо','enum','','customer','product',FALSE,TRUE,'Назначение и конструкция кашпо'),
 ('material','Материал','enum','','customer','product',FALSE,TRUE,'Основной материал товара'),
 ('color','Цвет','text','','customer','product',FALSE,TRUE,'Цвет по карточке производителя'),
 ('shape','Форма','enum','','customer','product',FALSE,TRUE,'Геометрическая форма'),
 ('product_diameter_cm','Диаметр','number','см','customer','variant',FALSE,TRUE,'Наружный диаметр товара'),
 ('product_height_cm','Высота товара','number','см','customer','variant',FALSE,TRUE,'Высота самого товара'),
 ('inner_diameter_cm','Внутренний диаметр','number','см','customer','variant',FALSE,TRUE,'Максимальный диаметр вставного горшка'),
 ('drainage_hole','Дренажное отверстие','boolean','','customer','product',FALSE,TRUE,'Есть отверстие для слива воды'),
 ('usage_area','Размещение','enum','','customer','product',FALSE,TRUE,'Для дома, улицы или обоих вариантов'),
 ('fertilizer_form','Форма удобрения','enum','','customer','product',FALSE,TRUE,'Физическая форма выпуска'),
 ('fertilizer_basis','Тип удобрения','enum','','customer','product',FALSE,TRUE,'Минеральное, органическое или смешанное'),
 ('application_method','Способ применения','multi_enum','','customer','product',FALSE,TRUE,'Корневая или листовая подкормка'),
 ('npk_n','Азот (N)','number','%','customer','product',FALSE,TRUE,'Массовая доля азота'),
 ('npk_p','Фосфор (P)','number','%','customer','product',FALSE,TRUE,'Массовая доля фосфора'),
 ('npk_k','Калий (K)','number','%','customer','product',FALSE,TRUE,'Массовая доля калия'),
 ('application_rate','Норма расхода','text','','customer','product',FALSE,TRUE,'Дозировка по инструкции'),
 ('accessory_type','Тип аксессуара','enum','','customer','product',FALSE,TRUE,'Функциональная группа аксессуара'),
 ('product_length_cm','Длина товара','number','см','customer','variant',FALSE,TRUE,'Длина самого товара'),
 ('product_width_cm','Ширина товара','number','см','customer','variant',FALSE,TRUE,'Ширина самого товара'),
 ('quantity_per_pack','В упаковке','number','шт','customer','variant',FALSE,TRUE,'Количество единиц'),
 ('compatibility','Совместимость','text','','customer','product',FALSE,TRUE,'Подходящие растения или товары')
ON CONFLICT(code) DO UPDATE SET name=EXCLUDED.name,data_type=EXCLUDED.data_type,unit=EXCLUDED.unit,
 audience=EXCLUDED.audience,value_scope=EXCLUDED.value_scope,is_global=EXCLUDED.is_global,
 is_active=EXCLUDED.is_active,description=EXCLUDED.description;

INSERT INTO attribute_options(attribute_id,code,label,sort_order)
SELECT definition.id, option.code, option.label, option.sort_order
FROM attribute_definitions definition
JOIN (VALUES
 ('soil_type','ready_mix','Готовый грунт',10),('soil_type','component','Компонент',20),('soil_type','drainage','Дренаж',30),('soil_type','mineral_substrate','Минеральный субстрат',40),
 ('target_plant','universal','Универсальный',10),('target_plant','indoor','Комнатные растения',20),('target_plant','palms','Пальмы',30),('target_plant','ficus','Фикусы',40),('target_plant','aroids','Ароидные',50),('target_plant','orchids','Орхидеи',60),('target_plant','succulents_cacti','Кактусы и суккуленты',70),('target_plant','citrus','Цитрусовые',80),('target_plant','violets','Фиалки',90),('target_plant','acid_loving','Кислолюбивые',100),('target_plant','seedlings','Рассада',110),
 ('pot_type','cachepot','Кашпо',10),('pot_type','planting_pot','Горшок',20),('pot_type','planter','Вазон',30),('pot_type','hanging','Подвесное',40),('pot_type','self_watering','С автополивом',50),
 ('material','ceramic','Керамика',10),('material','plastic','Пластик',20),('material','terracotta','Терракота',30),('material','concrete','Бетон',40),('material','metal','Металл',50),('material','glass','Стекло',60),('material','wood','Дерево',70),('material','fiberstone','Файберстоун',80),('material','textile','Текстиль',90),
 ('shape','round','Круглая',10),('shape','square','Квадратная',20),('shape','rectangular','Прямоугольная',30),('shape','oval','Овальная',40),('shape','other','Другая',50),
 ('usage_area','indoor','Для дома',10),('usage_area','outdoor','Для улицы',20),('usage_area','both','Для дома и улицы',30),
 ('fertilizer_form','liquid','Жидкость',10),('fertilizer_form','granules','Гранулы',20),('fertilizer_form','powder','Порошок',30),('fertilizer_form','sticks','Палочки',40),('fertilizer_form','tablets','Таблетки',50),('fertilizer_form','spray','Спрей',60),
 ('fertilizer_basis','mineral','Минеральное',10),('fertilizer_basis','organic','Органическое',20),('fertilizer_basis','organomineral','Органоминеральное',30),('fertilizer_basis','microbial','Микробиологическое',40),
 ('application_method','root','Корневая подкормка',10),('application_method','foliar','По листу',20),('application_method','soil_mixing','Внесение в грунт',30),
 ('accessory_type','support','Опоры и подвязки',10),('accessory_type','tool','Инструменты',20),('accessory_type','watering','Полив',30),('accessory_type','care','Уход',40),('accessory_type','protection','Защита',50),('accessory_type','decor','Декор',60),('accessory_type','propagation','Размножение',70)
) option(attribute_code,code,label,sort_order) ON option.attribute_code=definition.code
ON CONFLICT(attribute_id,code) DO UPDATE SET label=EXCLUDED.label,sort_order=EXCLUDED.sort_order;

-- Derived/legacy classifiers are not universal storefront characteristics.
UPDATE attribute_definitions SET is_global=FALSE WHERE code IN ('height_class','plant_type','toxicity');

-- Centralise assignments at five roots so descendants inherit one reviewed
-- schema. Removing assignments never removes filled attribute values.
WITH RECURSIVE roots AS (
 SELECT id,slug FROM categories WHERE slug IN ('plants','soil','fertilizer','pots','accessories')
 UNION ALL SELECT child.id,roots.slug FROM categories child JOIN roots ON child.parent_id=roots.id
), managed AS (
 SELECT id FROM attribute_definitions WHERE code IN (
  'height_cm','pot_diameter_cm','light_level','watering','humidity','care_level','toxicity','pet_safety','placement','growth_habit','plant_type','height_class','flowering',
  'soil_type','target_plant','volume_l','volume_ml','net_weight_g','composition','ph_min','ph_max','contains_fertilizer',
  'pot_type','material','color','shape','product_diameter_cm','product_height_cm','inner_diameter_cm','drainage_hole','usage_area',
  'fertilizer_form','fertilizer_basis','application_method','npk_n','npk_p','npk_k','application_rate',
  'accessory_type','product_length_cm','product_width_cm','quantity_per_pack','compatibility',
  'package_length_cm','package_width_cm','package_height_cm','package_weight_grams'))
DELETE FROM category_attributes assignment USING roots,managed
WHERE assignment.category_id=roots.id AND assignment.attribute_id=managed.id;

WITH schema(root_slug,code,required,filterable,pdp,badge,sort_order,summary,summary_position,characteristics) AS (VALUES
 ('plants','height_cm',TRUE,TRUE,TRUE,FALSE,10,TRUE,1,TRUE),('plants','pot_diameter_cm',TRUE,TRUE,TRUE,FALSE,20,TRUE,2,TRUE),
 ('plants','light_level',FALSE,TRUE,TRUE,TRUE,30,TRUE,3,TRUE),('plants','watering',FALSE,TRUE,TRUE,TRUE,40,TRUE,4,TRUE),
 ('plants','humidity',FALSE,FALSE,TRUE,FALSE,50,FALSE,NULL,TRUE),('plants','care_level',FALSE,TRUE,TRUE,TRUE,60,TRUE,5,TRUE),
 ('plants','pet_safety',FALSE,TRUE,TRUE,TRUE,70,FALSE,NULL,TRUE),('plants','growth_habit',FALSE,FALSE,TRUE,FALSE,80,FALSE,NULL,TRUE),
 ('plants','placement',FALSE,TRUE,TRUE,FALSE,90,FALSE,NULL,TRUE),('plants','flowering',FALSE,FALSE,TRUE,FALSE,100,FALSE,NULL,TRUE),
 ('soil','soil_type',FALSE,TRUE,TRUE,FALSE,10,TRUE,1,TRUE),('soil','target_plant',FALSE,TRUE,TRUE,TRUE,20,TRUE,2,TRUE),('soil','volume_l',FALSE,TRUE,TRUE,FALSE,30,TRUE,3,TRUE),('soil','net_weight_g',FALSE,FALSE,TRUE,FALSE,40,FALSE,NULL,TRUE),('soil','composition',FALSE,FALSE,TRUE,FALSE,50,FALSE,NULL,TRUE),('soil','ph_min',FALSE,FALSE,TRUE,FALSE,60,FALSE,NULL,TRUE),('soil','ph_max',FALSE,FALSE,TRUE,FALSE,70,FALSE,NULL,TRUE),('soil','contains_fertilizer',FALSE,FALSE,TRUE,FALSE,80,FALSE,NULL,TRUE),
 ('pots','pot_type',FALSE,TRUE,TRUE,TRUE,10,TRUE,1,TRUE),('pots','material',FALSE,TRUE,TRUE,FALSE,20,TRUE,2,TRUE),('pots','color',FALSE,FALSE,TRUE,FALSE,30,FALSE,NULL,TRUE),('pots','shape',FALSE,TRUE,TRUE,FALSE,40,FALSE,NULL,TRUE),('pots','product_diameter_cm',FALSE,TRUE,TRUE,FALSE,50,TRUE,3,TRUE),('pots','product_height_cm',FALSE,FALSE,TRUE,FALSE,60,FALSE,NULL,TRUE),('pots','inner_diameter_cm',FALSE,FALSE,TRUE,FALSE,70,TRUE,4,TRUE),('pots','volume_l',FALSE,FALSE,TRUE,FALSE,80,FALSE,NULL,TRUE),('pots','drainage_hole',FALSE,TRUE,TRUE,FALSE,90,FALSE,NULL,TRUE),('pots','usage_area',FALSE,TRUE,TRUE,FALSE,100,FALSE,NULL,TRUE),
 ('fertilizer','fertilizer_form',FALSE,TRUE,TRUE,TRUE,10,TRUE,1,TRUE),('fertilizer','fertilizer_basis',FALSE,TRUE,TRUE,FALSE,20,TRUE,2,TRUE),('fertilizer','target_plant',FALSE,TRUE,TRUE,TRUE,30,TRUE,3,TRUE),('fertilizer','application_method',FALSE,FALSE,TRUE,FALSE,40,FALSE,NULL,TRUE),('fertilizer','volume_ml',FALSE,TRUE,TRUE,FALSE,50,TRUE,4,TRUE),('fertilizer','net_weight_g',FALSE,FALSE,TRUE,FALSE,60,FALSE,NULL,TRUE),('fertilizer','npk_n',FALSE,FALSE,TRUE,FALSE,70,FALSE,NULL,TRUE),('fertilizer','npk_p',FALSE,FALSE,TRUE,FALSE,80,FALSE,NULL,TRUE),('fertilizer','npk_k',FALSE,FALSE,TRUE,FALSE,90,FALSE,NULL,TRUE),('fertilizer','application_rate',FALSE,FALSE,TRUE,FALSE,100,FALSE,NULL,TRUE),
 ('accessories','accessory_type',FALSE,TRUE,TRUE,TRUE,10,TRUE,1,TRUE),('accessories','material',FALSE,TRUE,TRUE,FALSE,20,TRUE,2,TRUE),('accessories','color',FALSE,FALSE,TRUE,FALSE,30,FALSE,NULL,TRUE),('accessories','product_length_cm',FALSE,FALSE,TRUE,FALSE,40,FALSE,NULL,TRUE),('accessories','product_width_cm',FALSE,FALSE,TRUE,FALSE,50,FALSE,NULL,TRUE),('accessories','product_height_cm',FALSE,FALSE,TRUE,FALSE,60,FALSE,NULL,TRUE),('accessories','quantity_per_pack',FALSE,FALSE,TRUE,FALSE,70,FALSE,NULL,TRUE),('accessories','compatibility',FALSE,FALSE,TRUE,FALSE,80,FALSE,NULL,TRUE)
), resolved AS (SELECT category.id category_id,definition.id attribute_id,schema.* FROM schema JOIN categories category ON category.slug=schema.root_slug JOIN attribute_definitions definition ON definition.code=schema.code)
INSERT INTO category_attributes(category_id,attribute_id,is_required,is_filterable,show_on_pdp,is_badge,sort_order,show_in_summary,summary_position,show_in_characteristics,is_excluded)
SELECT category_id,attribute_id,required,filterable,pdp,badge,sort_order,summary,summary_position,characteristics,FALSE FROM resolved;

-- Technical logistics dimensions apply to every merchandise root but never leak
-- onto the customer PDP.
INSERT INTO category_attributes(category_id,attribute_id,is_required,is_filterable,show_on_pdp,is_badge,sort_order,show_in_summary,summary_position,show_in_characteristics,is_excluded)
SELECT category.id,definition.id,FALSE,FALSE,FALSE,FALSE,900,FALSE,NULL,FALSE,FALSE
FROM categories category CROSS JOIN attribute_definitions definition
WHERE category.slug IN ('plants','soil','fertilizer','pots','accessories')
 AND definition.code IN ('package_length_cm','package_width_cm','package_height_cm','package_weight_grams');

-- Replace generic plant-only filters with scoped controls. Codes stay stable
-- for existing plant URLs; category_id prevents leakage into other departments.
UPDATE catalog_filters filter SET category_id=category.id,is_active=TRUE
FROM categories category WHERE category.slug='plants' AND filter.code IN ('light','watering','care','pot','pets');
UPDATE catalog_filters SET is_active=FALSE WHERE code='plant-type';
INSERT INTO catalog_filters(code,title,attribute_id,category_id,display_mode,sort_order,is_active)
SELECT seed.code,seed.title,definition.id,category.id,seed.mode,seed.sort_order,TRUE
FROM (VALUES
 ('soil-type','Тип грунта','soil_type','soil','select',110),('soil-target','Для растений','target_plant','soil','select',120),('soil-volume','Объём','volume_l','soil','range',130),
 ('pot-type','Тип кашпо','pot_type','pots','select',210),('pot-material','Материал','material','pots','select',220),('pot-diameter','Диаметр','product_diameter_cm','pots','range',230),('pot-drainage','Дренаж','drainage_hole','pots','chips',240),
 ('fertilizer-form','Форма','fertilizer_form','fertilizer','select',310),('fertilizer-basis','Тип','fertilizer_basis','fertilizer','select',320),('fertilizer-target','Для растений','target_plant','fertilizer','select',330),
 ('accessory-type','Тип аксессуара','accessory_type','accessories','select',410),('accessory-material','Материал','material','accessories','select',420)
) seed(code,title,attribute_code,category_slug,mode,sort_order)
JOIN attribute_definitions definition ON definition.code=seed.attribute_code JOIN categories category ON category.slug=seed.category_slug
ON CONFLICT(code) DO UPDATE SET title=EXCLUDED.title,attribute_id=EXCLUDED.attribute_id,category_id=EXCLUDED.category_id,display_mode=EXCLUDED.display_mode,sort_order=EXCLUDED.sort_order,is_active=TRUE;
