-- Exact PDF X-code and public-name guarded pot diameters; no heights inferred.
CREATE TEMP TABLE curated_pdf_pot_diameter (product_code bigint PRIMARY KEY, expected_name text NOT NULL, saby_code text NOT NULL, diameter_cm numeric NOT NULL CHECK (diameter_cm > 0)) ON COMMIT DROP;
INSERT INTO curated_pdf_pot_diameter(product_code,expected_name,saby_code,diameter_cm) VALUES
  (852,'Антуриум Блэк Лав (Andreanum «Black Love») D17','X354225507',17),
  (855,'Антуриум Мелодия Ибис (Anthurium Melodia Ibis) D17','X960621501',17),
  (856,'Диффенбахия Reflector Д22','X941618637',22),
  (857,'Замиокулькус Roundleaf D22','X450485643',22),
  (865,'Кордилина  Киви D10','X775675619',10),
  (860,'Нефролепис «Тедди Джуниор» D17','X494837501',17),
  (867,'Традесканция Зебрина D 10','X956843406',10),
  (861,'Хлорофитум кудрявый D10','X469818071',10),
  (854,'Адениум Пустынная роза D13','X269652448',13),
  (862,'Алоказия D6 3шт','X4476771',6),
  (863,'Амариллис D12','X6694188',12),
  (851,'Антуриум Блэк Бьюти (Anthurium Black Beauty) D17','X113534490',17),
  (853,'Араукария 20 см D10','X922724685',10),
  (864,'Бонсай Фисташка D15','X431642354',15),
  (866,'Лиметта Пурша на штамбе D15','X1140710',15);
UPDATE product_variants AS variant SET pot_diameter_cm=pdf.diameter_cm,updated_at=CURRENT_TIMESTAMP
FROM curated_pdf_pot_diameter AS pdf JOIN products AS product ON product.product_code=pdf.product_code
WHERE variant.product_id=product.id AND product.catalog_section='plants' AND product.name=pdf.expected_name
  AND variant.pot_diameter_cm IS NULL AND variant.is_active<>0 AND variant.archived_at IS NULL
  AND (SELECT count(*) FROM product_variants AS sibling WHERE sibling.product_id=product.id AND sibling.is_active<>0 AND sibling.archived_at IS NULL)=1
  AND EXISTS (SELECT 1 FROM product_external_ids AS external WHERE external.product_id=product.id AND external.provider='saby' AND external.id_type='code' AND external.external_id=pdf.saby_code);
