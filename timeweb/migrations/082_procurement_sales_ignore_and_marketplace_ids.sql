-- Obsolete marketplace cards can be excluded from procurement without
-- deleting sales history. The operation is reversible in the admin UI.
CREATE TABLE IF NOT EXISTS procurement_ignored_sales_products (
  channel TEXT NOT NULL CHECK (channel IN ('saby','wb','ozon')),
  external_product_id TEXT NOT NULL,
  ignored_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  ignored_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(channel, external_product_id)
);

-- A product can only have one current mapping of a given marketplace type.
-- The old trigger upserted by external_id only and could collide with the
-- independent product/provider uniqueness constraint when a manager moved a
-- card. Replace the current mapping atomically before recording the new one.
CREATE OR REPLACE FUNCTION sync_marketplace_external_ids() RETURNS trigger AS $$
DECLARE mapped_product BIGINT; mapped_variant BIGINT;
BEGIN
  SELECT p.id,pv.id INTO mapped_product,mapped_variant
  FROM products p LEFT JOIN LATERAL (
    SELECT id FROM product_variants WHERE product_id=p.id ORDER BY is_active DESC,id LIMIT 1
  ) pv ON TRUE WHERE p.saby_id=NEW.saby_id;
  IF mapped_product IS NULL THEN RETURN NEW; END IF;

  IF NEW.wb_nm_id IS NOT NULL THEN
    DELETE FROM product_external_ids WHERE product_id=mapped_product AND provider='wildberries' AND id_type='sku';
    INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id)
    VALUES(mapped_product,mapped_variant,'wildberries','sku',NEW.wb_nm_id::TEXT)
    ON CONFLICT(provider,id_type,external_id) DO UPDATE SET product_id=EXCLUDED.product_id,
      variant_id=EXCLUDED.variant_id,updated_at=CURRENT_TIMESTAMP;
  END IF;
  IF NULLIF(BTRIM(NEW.wb_vendor_code),'') IS NOT NULL THEN
    DELETE FROM product_external_ids WHERE product_id=mapped_product AND provider='wildberries' AND id_type='vendor_code';
    INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id)
    VALUES(mapped_product,mapped_variant,'wildberries','vendor_code',BTRIM(NEW.wb_vendor_code))
    ON CONFLICT(provider,id_type,external_id) DO UPDATE SET product_id=EXCLUDED.product_id,
      variant_id=EXCLUDED.variant_id,updated_at=CURRENT_TIMESTAMP;
  END IF;
  IF NULLIF(BTRIM(NEW.ozon_offer_id),'') IS NOT NULL THEN
    DELETE FROM product_external_ids WHERE product_id=mapped_product AND provider='ozon' AND id_type='offer_id';
    INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id)
    VALUES(mapped_product,mapped_variant,'ozon','offer_id',BTRIM(NEW.ozon_offer_id))
    ON CONFLICT(provider,id_type,external_id) DO UPDATE SET product_id=EXCLUDED.product_id,
      variant_id=EXCLUDED.variant_id,updated_at=CURRENT_TIMESTAMP;
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql;
