-- Replace temporary numbered labels with dimensions verified against the
-- original positive-stock Saby workbook (name + price + stock identity).
CREATE TEMP TABLE verified_pot_variants(
 sku text PRIMARY KEY,label text,volume_l numeric,diameter_cm numeric,height_cm numeric
) ON COMMIT DROP;
INSERT INTO verified_pot_variants VALUES
 ('512','D20 · H12 · 2,4 л',2.4,20,12),('827','D26 · H16 · 5,3 л',5.3,26,16),
 ('776','25 × 25 см · 11,6 л',11.6,25,NULL),('786','25 × 25 × 47 см · 19,4 л',19.4,25,47),
 ('562','2,5 л',2.5,NULL,NULL),('792','1,5 л',1.5,NULL,NULL),
 ('624','4,4 л',4.4,NULL,NULL),('628','3,1 л',3.1,NULL,NULL),
 ('779','D14 · H14 · 2,1 л',2.1,14,14),('780','D19 · H19 · 5,5 л',5.5,19,19),
 ('797','9,1 л',9.1,NULL,NULL),('800','11,5 л',11.5,NULL,NULL),
 ('585','3,7 л',3.7,NULL,NULL),('625','5,6 л',5.6,NULL,NULL),('812','9,1 л',9.1,NULL,NULL),
 ('617','2,5 л',2.5,NULL,NULL),('619','4 л',4,NULL,NULL),('627','5,8 л',5.8,NULL,NULL),
 ('559','0,5 л',0.5,NULL,NULL),('584','1 л',1,NULL,NULL),
 ('608','D9',NULL,9,NULL),('615','D12,5',NULL,12.5,NULL),
 ('571','1,3 л',1.3,NULL,NULL),('589','2 л',2,NULL,NULL),
 ('574','0,33 л',0.33,NULL,NULL),('577','0,5 л',0.5,NULL,NULL),
 ('556','D13,5 · H13 · 1,5 л',1.5,13.5,13),('686','D17 · 3,2 л',3.2,17,NULL),
 ('621','2,5 л',2.5,NULL,NULL),('789','3,7 л',3.7,NULL,NULL),
 ('497','500 мл',0.5,NULL,NULL),('671','100 мл',0.1,NULL,NULL),
 ('492','19 × 19 × 20,5 см · 5,5 л',5.5,19,20.5),('501','12,5 × 12,5 см · 1,5 л',1.5,12.5,NULL),
 ('572','550 мл',0.55,NULL,NULL),('822','330 мл',0.33,NULL,NULL),
 ('583','5,8 л',5.8,NULL,NULL),('658','9 л',9,NULL,NULL),('743','1,5 л',1.5,NULL,NULL);

UPDATE product_variants variant SET label=source.label,updated_at=CURRENT_TIMESTAMP
FROM verified_pot_variants source WHERE variant.sku=source.sku;

INSERT INTO variant_attribute_values(variant_id,attribute_id,value,source,updated_at)
SELECT variant.id,definition.id,to_jsonb(CASE definition.code
 WHEN 'volume_l' THEN source.volume_l WHEN 'product_diameter_cm' THEN source.diameter_cm
 WHEN 'product_height_cm' THEN source.height_cm END),'import',CURRENT_TIMESTAMP
FROM verified_pot_variants source JOIN product_variants variant ON variant.sku=source.sku
JOIN attribute_definitions definition ON
 (definition.code='volume_l' AND source.volume_l IS NOT NULL) OR
 (definition.code='product_diameter_cm' AND source.diameter_cm IS NOT NULL) OR
 (definition.code='product_height_cm' AND source.height_cm IS NOT NULL)
ON CONFLICT(variant_id,attribute_id) DO UPDATE SET value=EXCLUDED.value,source='import',updated_at=CURRENT_TIMESTAMP;

DO $validate$
BEGIN
 IF EXISTS(SELECT 1 FROM products WHERE product_code=619 AND name ILIKE '%деко твин%')
    AND (SELECT count(*) FROM verified_pot_variants source JOIN product_variants variant ON variant.sku=source.sku)
      <> (SELECT count(*) FROM verified_pot_variants) THEN
   RAISE EXCEPTION 'not every verified Saby pot SKU was resolved';
 END IF;
END;
$validate$;
