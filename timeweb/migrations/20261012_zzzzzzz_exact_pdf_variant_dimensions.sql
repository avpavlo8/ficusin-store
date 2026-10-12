-- Dimensions explicitly printed in the owner's PDF, guarded by site SKU,
-- exact site name, exact Saby X-code, one active variant and blank PIM value.
CREATE TEMP TABLE exact_pdf_variant_dimension (
  product_code bigint PRIMARY KEY,
  expected_name text NOT NULL,
  saby_code text NOT NULL,
  attribute_code text NOT NULL,
  dimension_cm numeric NOT NULL CHECK (dimension_cm > 0)
) ON COMMIT DROP;
INSERT INTO exact_pdf_variant_dimension(product_code,expected_name,saby_code,attribute_code,dimension_cm) VALUES
  (1,'Аглаонема в асс','X1150532','pot_diameter_cm',12),
  (510,'Глоксиния в горшке','X1933443','pot_diameter_cm',14),
  (853,'Араукария 20 см D10','X922724685','height_cm',20);

INSERT INTO variant_attribute_values(variant_id,attribute_id,value,source,updated_at)
SELECT variant.id,definition.id,to_jsonb(pdf.dimension_cm),'import',CURRENT_TIMESTAMP
FROM exact_pdf_variant_dimension AS pdf
JOIN products AS product ON product.product_code=pdf.product_code
JOIN product_variants AS variant ON variant.product_id=product.id
JOIN attribute_definitions AS definition ON definition.code=pdf.attribute_code AND definition.value_scope='variant'
WHERE product.catalog_section='plants' AND product.name=pdf.expected_name
  AND variant_numeric_attribute(variant.id,pdf.attribute_code) IS NULL
  AND variant.is_active<>0 AND variant.archived_at IS NULL
  AND (SELECT count(*) FROM product_variants AS sibling WHERE sibling.product_id=product.id AND sibling.is_active<>0 AND sibling.archived_at IS NULL)=1
  AND EXISTS (SELECT 1 FROM product_external_ids AS external WHERE external.product_id=product.id AND external.provider='saby' AND external.id_type='code' AND external.external_id=pdf.saby_code)
ON CONFLICT(variant_id,attribute_id) DO UPDATE
SET value=EXCLUDED.value,source='import',updated_at=CURRENT_TIMESTAMP
WHERE variant_attribute_values.value IN ('null'::jsonb,'""'::jsonb);
