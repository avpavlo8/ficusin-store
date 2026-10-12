-- The owner's PDF specifies D12 for X1150532/SKU 1. Its old PIM value is
-- numeric zero, so the blank-only migration could not replace it. A zero
-- diameter is not a physical pot size; keep the strict identity guard.
UPDATE variant_attribute_values AS value
SET value=to_jsonb(12::numeric), source='import', updated_at=CURRENT_TIMESTAMP
FROM product_variants AS variant
JOIN products AS product ON product.id=variant.product_id
JOIN attribute_definitions AS definition ON definition.code='pot_diameter_cm' AND definition.value_scope='variant'
WHERE value.variant_id=variant.id AND value.attribute_id=definition.id
  AND product.product_code=1 AND product.name='Аглаонема в асс'
  AND product.catalog_section='plants' AND product.status='published'
  AND variant.sku='1' AND variant.is_active<>0 AND variant.archived_at IS NULL
  AND variant_numeric_attribute(variant.id,'pot_diameter_cm')=0
  AND (SELECT count(*) FROM product_variants AS sibling WHERE sibling.product_id=product.id AND sibling.is_active<>0 AND sibling.archived_at IS NULL)=1
  AND EXISTS (SELECT 1 FROM product_external_ids AS external WHERE external.product_id=product.id AND external.provider='saby' AND external.id_type='code' AND external.external_id='X1150532');

-- RHS confirms the exact commercial cultivar already named on card #140.
-- https://www.rhs.org.uk/plants/251695/microsorum-musifolium-crocodyllus/details
UPDATE products AS product
SET latin_name='Microsorum musifolium ''Crocodyllus''', updated_at=CURRENT_TIMESTAMP
WHERE product.product_code=140 AND product.name='Микросорум Крокодилус'
  AND product.catalog_section='plants' AND product.status='published'
  AND product.latin_name='Microsorum'
  AND EXISTS (SELECT 1 FROM product_variants AS variant
    JOIN product_external_ids AS external ON external.product_id=product.id
      AND external.variant_id=variant.id AND external.provider='saby'
      AND external.id_type='code' AND external.external_id='X1520447'
    WHERE variant.product_id=product.id AND variant.sku='140');
