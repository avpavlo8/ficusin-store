-- Retail sales can identify one Saby card by catalogue.id, the visible X-code
-- or a UUID (externalId/hierarchicalId). Preserve every supplier alias instead
-- of forcing one of them into the canonical product key.
ALTER TABLE saby_nomenclature
  ADD COLUMN IF NOT EXISTS external_ids TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];

CREATE INDEX IF NOT EXISTS saby_nomenclature_external_ids_idx
  ON saby_nomenclature USING GIN (external_ids);

-- Existing numeric IDs and X-codes already contain enough information to
-- repair part of historical Saby sales immediately. UUID aliases are added by
-- the next catalogue snapshot and repaired by the same sync transaction.
WITH matched AS (
  SELECT DISTINCT ON (sale.sale_date, sale.external_product_id)
    sale.sale_date, sale.external_product_id,
    directory.variant_id, directory.saby_id, mapping.id AS mapping_id
  FROM procurement_sales_daily sale
  JOIN saby_nomenclature nomenclature ON
    nomenclature.saby_id=sale.external_product_id
    OR nomenclature.code=sale.external_product_id
    OR sale.external_product_id=ANY(nomenclature.external_ids)
  JOIN canonical_product_directory directory ON directory.saby_id=nomenclature.saby_id
  JOIN products product ON product.id=directory.product_id
    AND product.catalog_section='plants'
  LEFT JOIN LATERAL (
    SELECT external.id
    FROM product_external_ids external
    WHERE external.variant_id=directory.variant_id
      AND external.provider='saby'
      AND external.id_type IN ('id','code','alias')
      AND external.external_id=sale.external_product_id
      AND external.status IN ('active','legacy')
    ORDER BY (external.status='active') DESC, external.updated_at DESC
    LIMIT 1
  ) mapping ON TRUE
  WHERE sale.channel='saby'
  ORDER BY sale.sale_date, sale.external_product_id,
    directory.active DESC, directory.variant_id
)
UPDATE procurement_sales_daily sale SET
  saby_id=matched.saby_id,
  canonical_variant_id=matched.variant_id,
  external_mapping_id=matched.mapping_id,
  synced_at=CURRENT_TIMESTAMP
FROM matched
WHERE sale.channel='saby'
  AND sale.sale_date=matched.sale_date
  AND sale.external_product_id=matched.external_product_id;
