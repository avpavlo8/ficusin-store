BEGIN;
CREATE TEMP TABLE migration_fixture(id bigint);
INSERT INTO migration_fixture SELECT id FROM categories WHERE slug='plants';
INSERT INTO products(name,slug,status,catalog_section,category_id) VALUES('Local migration fixture','local-migration-proof','published','plants',(SELECT id FROM migration_fixture));
UPDATE migration_fixture SET id=(SELECT id FROM products WHERE slug='local-migration-proof');
INSERT INTO product_variants(product_id,sku,label,is_active,base_price_minor) SELECT id,'999999999999999','D12',1,100000 FROM migration_fixture;
INSERT INTO product_attribute_values(product_id,attribute_id,value)
SELECT f.id,d.id,s.value FROM migration_fixture f CROSS JOIN (VALUES
 ('storefront_traits','["partial_shade","sunny_window","trailing","support_climber","tree_form","fruiting","warm_bonsai","ornamental_bloom","patterned_foliage"]'::jsonb),
 ('light_level','"diffused"'::jsonb),('growth_habit','"upright"'::jsonb),('flowering','false'::jsonb),
 ('placement','["bathroom"]'::jsonb),('humidity','"high"'::jsonb)
) s(code,value) JOIN attribute_definitions d ON d.code=s.code;
INSERT INTO attribute_definitions(code,name,data_type,unit,audience,value_scope,is_active,is_global) VALUES('winter_notes_fixture','Зимовка','text','','customer','product',TRUE,TRUE);
INSERT INTO product_attribute_values(product_id,attribute_id,value) SELECT f.id,d.id,'"0…+8 °C; холодная лоджия"'::jsonb FROM migration_fixture f CROSS JOIN attribute_definitions d WHERE d.code='winter_notes_fixture';
UPDATE collections SET mode='dynamic',rules='[{"attribute":"placement","operator":"contains","value":"bathroom"}]'::jsonb WHERE slug='bathroom';
\i timeweb/migrations/20261111_plant_characteristics_collections.sql
DO $$
DECLARE attrs jsonb; temp_text text;
BEGIN
 SELECT jsonb_object_agg(d.code,v.value) INTO attrs FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id WHERE product_id=(SELECT id FROM migration_fixture);
 IF NOT (attrs->'light_level' @> '["sunny","diffused","low_light"]'::jsonb) THEN RAISE EXCEPTION 'Light values lost: %',attrs; END IF;
 IF NOT (attrs->'growth_habit' @> '["upright","trailing","climbing","tree"]'::jsonb) THEN RAISE EXCEPTION 'Growth values lost'; END IF;
 IF attrs->'flowering'<>'false'::jsonb THEN RAISE EXCEPTION 'Explicit false was overwritten'; END IF;
 IF attrs->>'wintering_type'<>'warm' OR attrs->'fruiting'<>'true'::jsonb THEN RAISE EXCEPTION 'Reviewed traits not migrated'; END IF;
 IF NOT EXISTS(SELECT 1 FROM collection_products p JOIN collections c ON c.id=p.collection_id WHERE c.slug='bathroom' AND p.product_id=(SELECT id FROM migration_fixture)) THEN RAISE EXCEPTION 'Manual membership lost'; END IF;
 SELECT plant_passport->>'temperature' INTO temp_text FROM products WHERE id=(SELECT id FROM migration_fixture);
 IF temp_text<>'0…+8 °C; холодная лоджия' THEN RAISE EXCEPTION 'Old winter note lost: %',temp_text; END IF;
 IF (SELECT COUNT(*) FROM attribute_options o JOIN attribute_definitions d ON d.id=o.attribute_id WHERE d.code='wintering_type' AND o.is_active)<>3 THEN RAISE EXCEPTION 'Wrong number of winter options'; END IF;
END $$;
ROLLBACK;
