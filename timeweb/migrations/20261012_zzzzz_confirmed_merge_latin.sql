-- Source-backed botanical identities for two duplicate launch cards.
-- This changes display Latin only. It does not merge cards or alter SKU,
-- photos, prices, stock, attributes, content, or marketplace identities.
-- References:
-- https://www.rhs.org.uk/plants/317750/ficus-benjamina-green-kinky/details
-- https://www.rhs.org.uk/plants/304353/nephrolepis-exaltata-green-lady/details
WITH confirmed AS (
    SELECT * FROM (VALUES
        (486::bigint, 'Фикус Бенджамина "Грин Кинки"'::text,
         'Ficus benjamina'::text, 'Ficus benjamina ''Green Kinky'''::text,
         ARRAY['486:X1284382']::text[]),
        (506::bigint, 'Нефролепис «Грин Леди»'::text,
         'Nephrolepis ''Green Lady'''::text, 'Nephrolepis exaltata ''Green Lady'''::text,
         ARRAY['506:X1705322', '563:X3217362']::text[])
    ) AS rows(product_code, expected_name, old_latin, confirmed_latin, sku_saby_codes)
)
UPDATE products AS product
SET latin_name = confirmed.confirmed_latin, updated_at = CURRENT_TIMESTAMP
FROM confirmed
WHERE product.product_code = confirmed.product_code
  AND product.catalog_section = 'plants'
  AND product.status = 'published'
  AND product.name = confirmed.expected_name
  AND product.latin_name = confirmed.old_latin
  AND (
      SELECT array_agg(variant.sku || ':' || nomenclature.code ORDER BY variant.sku)
      FROM product_variants AS variant
      LEFT JOIN saby_nomenclature AS nomenclature ON nomenclature.saby_id = variant.saby_id
      WHERE variant.product_id = product.id
  ) = confirmed.sku_saby_codes;
