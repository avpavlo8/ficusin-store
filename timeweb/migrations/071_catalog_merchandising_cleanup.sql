-- Bring the imported catalogue to a sellable SPU/SKU shape. Only explicit,
-- reviewed groups are merged: no fuzzy matching is performed in production.

-- A product inherits the storefront section from its top-level category.
WITH RECURSIVE category_roots AS (
  SELECT id,parent_id,slug,slug AS root_slug FROM categories WHERE parent_id IS NULL
  UNION ALL
  SELECT child.id,child.parent_id,child.slug,root.root_slug
  FROM categories child JOIN category_roots root ON child.parent_id=root.id
)
UPDATE products product
SET catalog_section=CASE category_roots.root_slug
      WHEN 'soil' THEN 'soil' WHEN 'fertilizer' THEN 'fertilizer'
      WHEN 'pots' THEN 'pots' WHEN 'accessories' THEN 'accessories'
      ELSE 'plants' END,
    updated_at=CURRENT_TIMESTAMP
FROM category_roots WHERE category_roots.id=product.category_id;

-- Preserve dimensions as SKU facts before removing them from card titles.
UPDATE product_variants variant
SET pot_diameter_cm=COALESCE(variant.pot_diameter_cm,
      replace(NULLIF((regexp_match(product.name,'(?:^|[^[:alpha:]])[DdДд][[:space:]]*([0-9]+(?:[.,][0-9]+)?)'))[1],''),',','.')::numeric),
    height_cm=COALESCE(variant.height_cm,
      replace(NULLIF((regexp_match(product.name,'(?:^|[^0-9])([0-9]+(?:[.,][0-9]+)?)[[:space:]]*см(?:[^[:alpha:]]|$)'))[1],''),',','.')::numeric),
    label=CASE
      WHEN product.catalog_section='plants' AND product.name ~* '[DdДд][[:space:]]*[0-9]'
        THEN concat_ws(' · ',
          CASE WHEN product.name ~* '[0-9]+(?:[.,][0-9]+)?[[:space:]]*см' THEN
            replace((regexp_match(product.name,'([0-9]+(?:[.,][0-9]+)?)[[:space:]]*см'))[1],'.',',')||' см' END,
          'D'||replace((regexp_match(product.name,'[DdДд][[:space:]]*([0-9]+(?:[.,][0-9]+)?)'))[1],'.',','))
      WHEN product.catalog_section<>'plants' AND product.name ~* '[0-9]+(?:[.,][0-9]+)?[[:space:]]*(мл|л|кг|г)(?:[^[:alpha:]]|$)'
        THEN replace((regexp_match(product.name,'([0-9]+(?:[.,][0-9]+)?)[[:space:]]*(мл|л|кг|г)(?:[^[:alpha:]]|$)'))[1],'.',',')||' '||
             (regexp_match(product.name,'[0-9]+(?:[.,][0-9]+)?[[:space:]]*(мл|л|кг|г)(?:[^[:alpha:]]|$)'))[2]
      ELSE variant.label END,
    updated_at=CURRENT_TIMESTAMP
FROM products product
WHERE product.id=variant.product_id;

-- Objective package volume/weight attributes derived from the supplier title.
INSERT INTO variant_attribute_values(variant_id,attribute_id,value,source,updated_at)
SELECT variant.id,definition.id,to_jsonb(replace(measure.amount,',','.')::numeric),
       'import',CURRENT_TIMESTAMP
FROM products product JOIN product_variants variant ON variant.product_id=product.id
CROSS JOIN LATERAL (
  SELECT match[1] amount,lower(match[2]) unit
  FROM regexp_match(product.name,'([0-9]+(?:[.,][0-9]+)?)[[:space:]]*(мл|л|кг|г)(?:[^[:alpha:]]|$)','i') match
) measure
JOIN attribute_definitions definition ON definition.code=CASE
  WHEN measure.unit='л' THEN 'volume_l' WHEN measure.unit='мл' THEN 'volume_ml'
  WHEN measure.unit='кг' THEN 'net_weight_g' WHEN measure.unit='г' THEN 'net_weight_g' END
WHERE product.catalog_section IN ('soil','fertilizer','pots','accessories')
ON CONFLICT(variant_id,attribute_id) DO NOTHING;
UPDATE variant_attribute_values value
SET value=to_jsonb((value.value#>>'{}')::numeric*1000),updated_at=CURRENT_TIMESTAMP
FROM attribute_definitions definition,product_variants variant,products product
WHERE value.attribute_id=definition.id AND definition.code='net_weight_g'
  AND value.variant_id=variant.id AND variant.product_id=product.id
  AND product.name ~* '[0-9]+(?:[.,][0-9]+)?[[:space:]]*кг(?:[^[:alpha:]]|$)'
  AND value.source='import';

-- Reviewed SPUs. The first code is the canonical card; the remaining codes
-- become its purchasable variants.
CREATE TEMP TABLE merchandising_merge_groups(group_key text,product_code bigint,position int) ON COMMIT DROP;
INSERT INTO merchandising_merge_groups VALUES
 ('alocasia-polly',17,1),('alocasia-polly',18,2),
 ('asplenium-wave',43,1),('asplenium-wave',44,2),('beaucarnea',48,1),('beaucarnea',49,2),
 ('bonsai-azalea',51,1),('bonsai-azalea',52,2),('bonsai-zelkova',58,1),('bonsai-zelkova',59,2),
 ('bonsai-carmona',60,1),('bonsai-carmona',61,2),('bonsai-ligustrum',65,1),('bonsai-ligustrum',66,2),
 ('bonsai-metasequoia',68,1),('bonsai-metasequoia',69,2),('bonsai-juniper',70,1),('bonsai-juniper',71,2),
 ('bonsai-olive',72,1),('bonsai-olive',73,2),('bonsai-sageretia',79,1),('bonsai-sageretia',80,2),
 ('bonsai-ficus-retusa',84,1),('bonsai-ficus-retusa',85,2),('zamioculcas-zenzi',110,1),('zamioculcas-zenzi',111,2),
 ('microsorum',140,1),('microsorum',141,2),('monstera-deliciosa',149,1),('monstera-deliciosa',150,2),
 ('rhipsalis',170,1),('rhipsalis',171,2),('sansevieria-twist',175,1),('sansevieria-twist',176,2),
 ('poinsettia',168,1),('poinsettia',169,2),('hedera',230,1),('hedera',231,2),('hedera',232,3),
 ('hoya',234,1),('hoya',235,2),('hoya',236,3),('hoya',237,4),('cycas',238,1),('cycas',239,2),
 ('philodendron-brasil',226,1),('philodendron-brasil',227,2),
 ('terra-nova-aqua',331,1),('terra-nova-aqua',332,2),('terra-nova-aqua',333,3),('terra-nova-aqua',334,4),('terra-nova-aqua',335,5),
 ('terra-nova-new-earth',343,1),('terra-nova-new-earth',344,2),('terra-nova-new-earth',345,3),('terra-nova-new-earth',346,4),('terra-nova-new-earth',347,5),
 ('terra-vita-universal',353,1),('terra-vita-universal',354,2),('terra-vita-universal',355,3),('terra-vita-universal',356,4),
 ('terra-vita-flower',357,1),('terra-vita-flower',358,2),('living-earth-flower',383,1),('living-earth-flower',384,2),
 ('lemon-soil',387,1),('lemon-soil',388,2),('rose-soil',395,1),('rose-soil',396,2),
 ('deco-twin-anthracite',619,1),('deco-twin-anthracite',627,2),('deco-twin-anthracite',617,3),
 ('deco-twin-lavender',792,1),('deco-twin-lavender',562,2),('deco-twin-shade',743,1),('deco-twin-shade',583,2),('deco-twin-shade',658,3),
 ('latina-anthracite',621,1),('latina-anthracite',789,2),('latina-cream',585,1),('latina-cream',625,2),('latina-cream',812,3),
 ('latina-shade',797,1),('latina-shade',800,2),('orchid-pearl',571,1),('orchid-pearl',589,2),
 ('rossi-white',624,1),('rossi-white',628,2),('honeycomb-pot',574,1),('honeycomb-pot',577,2),
 ('cylinder-pot',584,1),('cylinder-pot',559,2),('printed-pot',822,1),('printed-pot',485,2),('printed-pot',572,3),
 ('printed-cup',671,1),('printed-cup',497,2),('delta-anthracite',686,1),('delta-anthracite',556,2),
 ('grand-black',492,1),('grand-black',501,2),('cylinder-silver',615,1),('cylinder-silver',608,2),
 ('london-cube',786,1),('london-cube',776,2),('nard-lilac',780,1),('nard-lilac',779,2),
 ('omega-white',512,1),('omega-white',827,2);

DO $merge$
DECLARE group_row record; source_id bigint; source_variant bigint;
BEGIN
 FOR group_row IN
   SELECT grouping.group_key,
     (array_agg(product.id ORDER BY grouping.position))[1] target_id,
     array_agg(product.id ORDER BY grouping.position) product_ids
   FROM merchandising_merge_groups grouping
   JOIN products product ON product.product_code=grouping.product_code
   GROUP BY grouping.group_key HAVING count(*)>1
 LOOP
  FOREACH source_id IN ARRAY group_row.product_ids LOOP
   IF source_id=group_row.target_id THEN CONTINUE; END IF;
   SELECT id INTO source_variant FROM product_variants WHERE product_id=source_id ORDER BY is_active DESC,id LIMIT 1;

   INSERT INTO product_attribute_values(product_id,attribute_id,value,source,updated_at)
   SELECT group_row.target_id,attribute_id,value,source,CURRENT_TIMESTAMP FROM product_attribute_values WHERE product_id=source_id
   ON CONFLICT(product_id,attribute_id) DO NOTHING;
   INSERT INTO collection_products(collection_id,product_id,sort_order)
   SELECT collection_id,group_row.target_id,sort_order FROM collection_products WHERE product_id=source_id ON CONFLICT DO NOTHING;
   DELETE FROM collection_products WHERE product_id=source_id;

   -- Two reviews for the same purchase are invalid; retain the canonical one.
   DELETE FROM product_reviews source_review
   USING product_reviews target_review
   WHERE source_review.product_id=source_id AND target_review.product_id=group_row.target_id
     AND source_review.customer_id=target_review.customer_id AND source_review.order_id=target_review.order_id;
   UPDATE product_reviews SET product_id=group_row.target_id WHERE product_id=source_id;
   UPDATE order_items SET product_id=group_row.target_id WHERE product_id=source_id;

   DELETE FROM catalog_ai_enrichment_jobs WHERE product_id=source_id;
   UPDATE product_variants SET product_id=group_row.target_id,updated_at=CURRENT_TIMESTAMP WHERE product_id=source_id;
   UPDATE product_external_ids SET product_id=group_row.target_id,variant_id=COALESCE(variant_id,source_variant),updated_at=CURRENT_TIMESTAMP WHERE product_id=source_id;
   UPDATE product_media SET product_id=group_row.target_id WHERE product_id=source_id;
   DELETE FROM products WHERE id=source_id;
  END LOOP;
 END LOOP;
END;
$merge$;

-- Clean customer-facing titles after measurements have become variant labels.
UPDATE products SET name=BTRIM(regexp_replace(regexp_replace(regexp_replace(regexp_replace(
  replace(replace(name,E'\n',' '),'Terra Nova Terra Nova','Terra Nova'),
  '[[:space:]]+[DdДд][[:space:]]*[0-9]+(?:[.,][0-9]+)?',' ','gi'),
  '[[:space:]]+[0-9]+(?:[.,][0-9]+)?[[:space:]]*см(?:[^[:alpha:]]|$)',' ','gi'),
  '[[:space:]]+[0-9]+(?:[.,][0-9]+)?[[:space:]]*(мл|л|кг|г)(?:[^[:alpha:]]|$)',' ','gi'),
  '[[:space:]]+',' ','g'), ' ,.-'),updated_at=CURRENT_TIMESTAMP;

-- Curated commercial wording for the main merged cards.
UPDATE products product SET name=names.name,search_text=names.name,updated_at=CURRENT_TIMESTAMP
FROM (VALUES
 (17::bigint,'Алоказия Полли'),(43,'Асплениум нидус «Криспи Вейв»'),(48,'Бокарнея (нолина)'),
 (110,'Замиокулькас «Зензи»'),(149,'Монстера делициоза'),(175,'Сансевиерия цилиндрическая «Твист»'),
 (168,'Пуансеттия (рождественская звезда)'),(226,'Филодендрон сканденс «Бразил»'),(238,'Цикас (саговник)'),
 (331,'Terra Nova Aqua — универсальный субстрат'),(343,'Terra Nova «Новая земля» — универсальный грунт'),
 (353,'Terra Vita «Живая земля» — универсальный грунт'),(357,'Terra Vita «Живая земля» — цветочный грунт')
) names(code,name) WHERE product.product_code=names.code;

-- Latin genus names are taxonomy, not generated marketing copy. Fill only
-- plant categories with an unambiguous botanical genus.
UPDATE products product SET latin_name=taxonomy.latin,updated_at=CURRENT_TIMESTAMP
FROM categories category JOIN (VALUES
 ('aglaonema','Aglaonema'),('alocasia','Alocasia'),('pineapple','Ananas'),('anthurium','Anthurium'),
 ('asplenium','Asplenium'),('beaucarnea','Beaucarnea'),('hibiscus','Hibiscus'),('davallia','Davallia'),
 ('dypsis','Dypsis'),('dieffenbachia','Dieffenbachia'),('dracaena','Dracaena'),('zamioculcas','Zamioculcas'),
 ('calathea','Goeppertia'),('clusia','Clusia'),('cordyline','Cordyline'),('crassula','Crassula'),
 ('senecio','Senecio'),('croton','Codiaeum'),('livistona','Livistona'),('maranta','Maranta'),
 ('microsorum','Microsorum'),('myrsine','Myrsine'),('monstera','Monstera'),('musa','Musa'),('nepenthes','Nepenthes'),
 ('nephrolepis','Nephrolepis'),('olive','Olea europaea'),('pachira','Pachira aquatica'),('peperomia','Peperomia'),
 ('platycerium','Platycerium'),('rhipsalis','Rhipsalis'),('sansevieria','Dracaena'),('syngonium','Syngonium'),
 ('spathiphyllum','Spathiphyllum'),('strelitzia','Strelitzia'),('tillandsia','Tillandsia'),
 ('tradescantia','Tradescantia'),('ficus','Ficus'),('philodendron','Philodendron'),('fittonia','Fittonia'),
 ('chamaedorea','Chamaedorea'),('hedera','Hedera'),('chlorophytum','Chlorophytum'),('hoya','Hoya'),
 ('cycas','Cycas revoluta'),('cyrtomium','Cyrtomium'),('schefflera','Heptapleurum'),('epipremnum','Epipremnum'),('yucca','Yucca')
) taxonomy(slug,latin) ON taxonomy.slug=category.slug
WHERE product.category_id=category.id AND NULLIF(BTRIM(product.latin_name),'') IS NULL;

-- Conservative genus-level care profiles. Existing manager-entered values
-- always win; these defaults only replace empty imported cards.
CREATE TEMP TABLE plant_category_profiles(slug text,light text,watering text,humidity text,care text,pet text,habit text) ON COMMIT DROP;
INSERT INTO plant_category_profiles VALUES
 ('aglaonema','diffused','moderate','medium','easy','caution','bushy'),
 ('alocasia','diffused','moderate','high','medium','caution','upright'),
 ('anthurium','diffused','moderate','high','medium','caution','bushy'),
 ('asplenium','diffused','moderate','high','medium','safe','rosette'),
 ('beaucarnea','sunny','rare','low','easy','safe','upright'),
 ('davallia','diffused','moderate','high','medium','safe','trailing'),
 ('dieffenbachia','diffused','moderate','medium','easy','caution','upright'),
 ('dracaena','diffused','moderate','medium','easy','caution','upright'),
 ('zamioculcas','low_light','rare','low','easy','caution','upright'),
 ('cactus','sunny','rare','low','easy','caution','upright'),
 ('calathea','diffused','moderate','high','demanding','safe','bushy'),
 ('crassula','sunny','rare','low','easy','caution','upright'),
 ('maranta','diffused','moderate','high','medium','safe','trailing'),
 ('microsorum','diffused','moderate','high','medium','safe','rosette'),
 ('monstera','diffused','moderate','medium','easy','caution','climbing'),
 ('nephrolepis','diffused','moderate','high','medium','safe','bushy'),
 ('orchid','diffused','moderate','high','medium','caution','rosette'),
 ('pachira','diffused','moderate','medium','easy','safe','upright'),
 ('peperomia','diffused','moderate','medium','easy','safe','bushy'),
 ('rhipsalis','diffused','rare','medium','easy','safe','trailing'),
 ('sansevieria','low_light','rare','low','easy','caution','upright'),
 ('syngonium','diffused','moderate','medium','easy','caution','climbing'),
 ('spathiphyllum','diffused','moderate','high','easy','caution','bushy'),
 ('strelitzia','sunny','moderate','medium','medium','caution','upright'),
 ('succulents','sunny','rare','low','easy','caution','rosette'),
 ('tradescantia','diffused','moderate','medium','easy','caution','trailing'),
 ('ficus','diffused','moderate','medium','easy','caution','upright'),
 ('philodendron','diffused','moderate','medium','easy','caution','climbing'),
 ('fittonia','diffused','moderate','high','medium','safe','trailing'),
 ('chamaedorea','diffused','moderate','high','easy','safe','upright'),
 ('hedera','diffused','moderate','medium','easy','caution','trailing'),
 ('chlorophytum','diffused','moderate','medium','easy','safe','bushy'),
 ('hoya','diffused','rare','medium','easy','caution','climbing'),
 ('cycas','sunny','moderate','medium','medium','caution','upright'),
 ('schefflera','diffused','moderate','medium','easy','caution','upright'),
 ('epipremnum','low_light','moderate','medium','easy','caution','climbing'),
 ('yucca','sunny','rare','low','easy','caution','upright');

UPDATE products product SET
 light_level=COALESCE(NULLIF(product.light_level,''),profile.light),
 watering=COALESCE(NULLIF(product.watering,''),profile.watering),
 care_level=COALESCE(NULLIF(product.care_level,''),profile.care),
 pet_safety=COALESCE(NULLIF(product.pet_safety,''),profile.pet),
 growth_habit=COALESCE(NULLIF(product.growth_habit,''),profile.habit),updated_at=CURRENT_TIMESTAMP
FROM categories category JOIN plant_category_profiles profile ON profile.slug=category.slug
WHERE product.category_id=category.id;

INSERT INTO product_attribute_values(product_id,attribute_id,value,source,updated_at)
SELECT product.id,definition.id,to_jsonb(CASE definition.code
 WHEN 'light_level' THEN profile.light WHEN 'watering' THEN profile.watering
 WHEN 'humidity' THEN profile.humidity WHEN 'care_level' THEN profile.care
 WHEN 'pet_safety' THEN profile.pet WHEN 'growth_habit' THEN profile.habit END),
 'import',CURRENT_TIMESTAMP
FROM products product JOIN categories category ON category.id=product.category_id
JOIN plant_category_profiles profile ON profile.slug=category.slug
JOIN attribute_definitions definition ON definition.code IN ('light_level','watering','humidity','care_level','pet_safety','growth_habit')
ON CONFLICT(product_id,attribute_id) DO NOTHING;

-- Unambiguous non-plant classifiers, derived from the actual product wording.
INSERT INTO product_attribute_values(product_id,attribute_id,value,source,updated_at)
SELECT product.id,definition.id,to_jsonb(CASE definition.code
 WHEN 'soil_type' THEN CASE WHEN product.name ~* 'zeoflora|цеолит|минеральн' THEN 'mineral_substrate' ELSE 'ready_mix' END
 WHEN 'pot_type' THEN CASE WHEN product.name ~* 'кашпо' THEN 'cachepot' ELSE 'planting_pot' END
 WHEN 'material' THEN CASE WHEN product.name ~* 'керам' THEN 'ceramic' WHEN product.name ~* 'терракот' THEN 'terracotta' WHEN product.name ~* 'бетон' THEN 'concrete' ELSE 'plastic' END END),
 'import',CURRENT_TIMESTAMP
FROM products product
JOIN attribute_definitions definition ON
 (product.catalog_section='soil' AND definition.code='soil_type') OR
 (product.catalog_section='pots' AND definition.code IN ('pot_type','material'))
ON CONFLICT(product_id,attribute_id) DO NOTHING;

DO $validate$
BEGIN
 IF EXISTS (
   WITH RECURSIVE category_roots AS (
    SELECT id,parent_id,slug,slug root_slug FROM categories WHERE parent_id IS NULL
    UNION ALL SELECT child.id,child.parent_id,child.slug,root.root_slug FROM categories child JOIN category_roots root ON child.parent_id=root.id)
   SELECT 1 FROM products product JOIN category_roots root ON root.id=product.category_id
   WHERE product.catalog_section<>CASE root.root_slug WHEN 'soil' THEN 'soil' WHEN 'fertilizer' THEN 'fertilizer' WHEN 'pots' THEN 'pots' WHEN 'accessories' THEN 'accessories' ELSE 'plants' END
 ) THEN RAISE EXCEPTION 'catalog section/category mismatch remains'; END IF;
 IF EXISTS (
   SELECT grouping.group_key FROM merchandising_merge_groups grouping JOIN products product ON product.product_code=grouping.product_code
   GROUP BY grouping.group_key HAVING count(*)>1
 ) THEN RAISE EXCEPTION 'reviewed product merge group remains split'; END IF;
END;
$validate$;

INSERT INTO admin_audit_log(actor_customer_id,actor_role,action,entity_type,entity_id,after_data)
VALUES(NULL,'system','catalogue.merchandising.cleanup','catalogue','071',jsonb_build_object(
 'mergedGroups',(SELECT count(DISTINCT group_key) FROM merchandising_merge_groups),
 'catalogSectionsFixed',(SELECT count(*) FROM products WHERE catalog_section IN ('soil','fertilizer','pots','accessories')),
 'latinNames',(SELECT count(*) FROM products WHERE latin_name<>'')));
