-- Canonical plant properties drive collections; editorial selections stay manual.
-- Existing PIM data and detailed care text are retained.

-- Preserve the current computed membership when making editorial lists manual.
INSERT INTO collection_products(collection_id,product_id,sort_order)
SELECT c.id,p.id,ROW_NUMBER() OVER (PARTITION BY c.id ORDER BY p.id)::integer
FROM collections c CROSS JOIN products p
WHERE c.slug IN ('bathroom','office','bedroom') AND c.mode='dynamic'
  AND collection_rules_match_product(p.id,c.rules)
ON CONFLICT DO NOTHING;
UPDATE collections SET mode='manual',rules='[]'::jsonb,updated_at=CURRENT_TIMESTAMP
WHERE slug IN ('bathroom','office','bedroom');

UPDATE attribute_definitions SET data_type='multi_enum',updated_at=CURRENT_TIMESTAMP
WHERE code IN ('light_level','growth_habit');
UPDATE product_attribute_values v SET value=jsonb_build_array(v.value)
FROM attribute_definitions d WHERE v.attribute_id=d.id
 AND d.code IN ('light_level','growth_habit') AND jsonb_typeof(v.value)='string';
UPDATE attribute_options o SET label='Прямое солнце' FROM attribute_definitions d
WHERE o.attribute_id=d.id AND d.code='light_level' AND o.code='sunny';
UPDATE attribute_definitions SET name='Цветущее',updated_at=CURRENT_TIMESTAMP
WHERE code='flowering';
INSERT INTO attribute_options(attribute_id,code,label,sort_order)
SELECT id,'tree','Деревце',60 FROM attribute_definitions WHERE code='growth_habit'
ON CONFLICT(attribute_id,code) DO NOTHING;
UPDATE attribute_options o SET label='На опоре' FROM attribute_definitions d
WHERE o.attribute_id=d.id AND d.code='growth_habit' AND o.code='climbing';

INSERT INTO attribute_definitions(code,name,data_type,unit,audience,value_scope,is_active,is_global,description)
VALUES ('leaf_coloring','Окраска листьев','enum','','customer','product',TRUE,FALSE,''),
 ('fruiting','Плодовое растение','boolean','','customer','product',TRUE,FALSE,''),
 ('wintering_type','Зимовка','enum','','customer','product',TRUE,FALSE,'')
ON CONFLICT(code) DO NOTHING;
INSERT INTO attribute_options(attribute_id,code,label,sort_order)
SELECT d.id,s.code,s.label,s.position FROM attribute_definitions d JOIN (VALUES
 ('leaf_coloring','plain','Однотонная',10),
 ('leaf_coloring','variegated','Вариегатная',20),
 ('leaf_coloring','patterned','Узорчатая',30),
 ('wintering_type','warm','Тёплая (+20…+27 °C)',10),
 ('wintering_type','cool','Прохладная (+10…+19 °C)',20),
 ('wintering_type','cold','Холодная (0…+9 °C)',30)
) s(attribute,code,label,position) ON d.code=s.attribute
ON CONFLICT(attribute_id,code) DO UPDATE SET label=EXCLUDED.label,is_active=TRUE;
INSERT INTO category_attributes(category_id,attribute_id,is_required,is_filterable,show_on_pdp,is_badge,sort_order,show_in_summary,summary_position,show_in_characteristics,is_excluded)
SELECT c.id,d.id,FALSE,TRUE,TRUE,FALSE,s.position,d.code='wintering_type',
 CASE WHEN d.code='wintering_type' THEN 8 ELSE NULL END,TRUE,FALSE
FROM categories c CROSS JOIN (VALUES ('flowering',90),('growth_habit',100),('leaf_coloring',110),('fruiting',120),('wintering_type',80)) s(code,position)
JOIN attribute_definitions d ON d.code=s.code WHERE c.slug='plants'
ON CONFLICT(category_id,attribute_id) DO UPDATE SET is_excluded=FALSE,show_in_characteristics=TRUE,
 show_in_summary=EXCLUDED.show_in_summary,summary_position=EXCLUDED.summary_position;

-- Carry reviewed legacy traits into the canonical properties. Do not invent
-- negatives or infer a winter temperature from category/name/free-form prose.
INSERT INTO product_attribute_values(product_id,attribute_id,value,source)
SELECT old.product_id,d.id,jsonb_agg(DISTINCT s.value),'local'
FROM product_attribute_values old JOIN attribute_definitions legacy ON old.attribute_id=legacy.id AND legacy.code='storefront_traits'
JOIN (VALUES ('partial_shade','light_level','low_light'),('sunny_window','light_level','sunny'),
 ('trailing','growth_habit','trailing'),('support_climber','growth_habit','climbing'),('tree_form','growth_habit','tree')) s(trait,code,value)
 ON old.value @> jsonb_build_array(s.trait)
JOIN attribute_definitions d ON d.code=s.code
GROUP BY old.product_id,d.id
ON CONFLICT(product_id,attribute_id) DO UPDATE SET
 value=(SELECT jsonb_agg(DISTINCT entry) FROM jsonb_array_elements(
   CASE WHEN jsonb_typeof(product_attribute_values.value)='array' THEN product_attribute_values.value
   ELSE jsonb_build_array(product_attribute_values.value) END || EXCLUDED.value) entry);
-- New rows of multivalued attributes must be arrays too.
UPDATE product_attribute_values v SET value=jsonb_build_array(v.value)
FROM attribute_definitions d WHERE v.attribute_id=d.id AND d.code IN ('light_level','growth_habit') AND jsonb_typeof(v.value)='string';
INSERT INTO product_attribute_values(product_id,attribute_id,value,source)
SELECT old.product_id,d.id,s.value,'local'
FROM product_attribute_values old JOIN attribute_definitions legacy ON old.attribute_id=legacy.id AND legacy.code='storefront_traits'
JOIN (VALUES ('ornamental_bloom','flowering','true'::jsonb),('fruiting','fruiting','true'::jsonb),
 ('patterned_foliage','leaf_coloring','"patterned"'::jsonb),('warm_bonsai','wintering_type','"warm"'::jsonb)) s(trait,code,value)
 ON old.value @> jsonb_build_array(s.trait)
JOIN attribute_definitions d ON d.code=s.code ON CONFLICT(product_id,attribute_id) DO NOTHING;

-- Old textual winter notes belong to the Temperature care topic. Preserve the
-- original stored attribute as history, but do not show duplicate winter fields.
UPDATE products p SET plant_passport=jsonb_set(COALESCE(p.plant_passport,'{}'::jsonb),'{temperature}',v.value)
FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id
WHERE p.id=v.product_id AND d.name ILIKE '%зимовк%' AND d.code<>'wintering_type'
 AND jsonb_typeof(v.value)='string' AND COALESCE(p.plant_passport->>'temperature','')='';
UPDATE category_attributes a SET show_in_summary=FALSE,show_in_characteristics=FALSE,show_on_pdp=FALSE
FROM attribute_definitions d WHERE a.attribute_id=d.id AND d.name ILIKE '%зимовк%' AND d.code<>'wintering_type';
UPDATE category_attributes a SET is_excluded=TRUE
FROM attribute_definitions d WHERE a.attribute_id=d.id AND d.code='storefront_traits';

-- Category predicates are derived from the actual category ancestry, with no
-- second editable 'bonsai' flag. Match all conditions on one visible sale SKU.
CREATE OR REPLACE FUNCTION collection_rules_match_variant(target_product BIGINT,target_variant BIGINT,rule_set JSONB)
RETURNS BOOLEAN LANGUAGE plpgsql STABLE AS $$
DECLARE rule JSONB; definition_id BIGINT; definition_scope TEXT; matched BOOLEAN;
BEGIN
 IF rule_set IS NULL OR jsonb_typeof(rule_set)<>'array' OR jsonb_array_length(rule_set)=0 THEN RETURN FALSE; END IF;
 IF NOT EXISTS(SELECT 1 FROM product_variants v WHERE v.id=target_variant AND v.product_id=target_product
   AND v.is_active=1 AND v.archived_at IS NULL AND NOT EXISTS(SELECT 1 FROM launch_hidden_extra_plant_variants h WHERE h.variant_id=v.id))
 THEN RETURN FALSE; END IF;
 FOR rule IN SELECT element FROM jsonb_array_elements(rule_set) element LOOP
  IF rule->>'attribute'='category' THEN
   WITH RECURSIVE ancestry AS (
    SELECT c.id,c.parent_id,c.slug FROM products p JOIN categories c ON c.id=p.category_id WHERE p.id=target_product
    UNION ALL SELECT c.id,c.parent_id,c.slug FROM categories c JOIN ancestry a ON a.parent_id=c.id
   ) SELECT CASE rule->>'operator'
    WHEN 'eq' THEN collection_rule_value_matches(COALESCE(jsonb_agg(slug),'[]'::jsonb),jsonb_set(rule,'{operator}','"contains"'::jsonb))
    WHEN 'neq' THEN NOT collection_rule_value_matches(COALESCE(jsonb_agg(slug),'[]'::jsonb),jsonb_set(rule,'{operator}','"contains"'::jsonb))
    ELSE collection_rule_value_matches(COALESCE(jsonb_agg(slug),'[]'::jsonb),rule) END INTO matched FROM ancestry;
  ELSE
   definition_id:=NULL; definition_scope:=NULL;
   SELECT id,value_scope INTO definition_id,definition_scope FROM attribute_definitions
    WHERE code=BTRIM(rule->>'attribute') AND is_active LIMIT 1;
   IF definition_id IS NULL THEN RETURN FALSE; END IF;
   IF definition_scope='product' THEN
    SELECT COALESCE(BOOL_OR(collection_rule_value_matches(value,rule)),FALSE) INTO matched
    FROM product_attribute_values WHERE product_id=target_product AND attribute_id=definition_id;
   ELSE
    SELECT COALESCE(BOOL_OR(collection_rule_value_matches(value,rule)),FALSE) INTO matched
    FROM variant_attribute_values WHERE variant_id=target_variant AND attribute_id=definition_id;
   END IF;
  END IF;
  IF NOT COALESCE(matched,FALSE) THEN RETURN FALSE; END IF;
 END LOOP;
 RETURN TRUE;
END;
$$;

UPDATE collections c SET rules=s.rules::jsonb,updated_at=CURRENT_TIMESTAMP FROM (VALUES
 ('easy','[{"attribute":"care_level","operator":"eq","value":"easy"}]'),
 ('dark','[{"attribute":"light_level","operator":"contains","value":"low_light"}]'),
 ('rare','[{"attribute":"watering","operator":"eq","value":"rare"}]'),
 ('pets','[{"attribute":"pet_safety","operator":"eq","value":"safe"}]'),
 ('flowering','[{"attribute":"flowering","operator":"eq","value":true}]'),
 ('sunny-window','[{"attribute":"light_level","operator":"contains","value":"sunny"}]'),
 ('trailing','[{"attribute":"growth_habit","operator":"contains","value":"trailing"}]'),
 ('tall','[{"attribute":"height_cm","operator":"gte","value":80}]'),
 ('compact','[{"attribute":"height_cm","operator":"gte","value":1},{"attribute":"height_cm","operator":"lte","value":40}]'),
 ('indoor-bonsai','[{"attribute":"category","operator":"contains","value":"bonsai"},{"attribute":"wintering_type","operator":"eq","value":"warm"}]'),
 ('variegated','[{"attribute":"leaf_coloring","operator":"in","value":["variegated","patterned"]}]'),
 ('vertical','[{"attribute":"growth_habit","operator":"contains","value":"climbing"}]'),
 ('home-trees','[{"attribute":"growth_habit","operator":"contains","value":"tree"},{"attribute":"wintering_type","operator":"eq","value":"warm"}]'),
 ('fruiting','[{"attribute":"fruiting","operator":"eq","value":true}]'),
 ('bonsai','[{"attribute":"category","operator":"contains","value":"bonsai"}]')
) s(slug,rules) WHERE c.slug=s.slug;
