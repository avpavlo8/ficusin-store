-- Keep procurement channel edits attached to their canonical variant.
CREATE OR REPLACE FUNCTION sync_marketplace_external_ids() RETURNS trigger AS $$
DECLARE mapped_product BIGINT; mapped_variant BIGINT;
BEGIN
  -- Resolve the actual SKU selected in procurement, including variants whose
  -- Saby identity differs from the parent product. Never pick its first size.
  SELECT directory.product_id, directory.variant_id INTO mapped_product, mapped_variant
  FROM canonical_product_directory directory
  WHERE directory.saby_id = NEW.saby_id AND directory.active
    AND (SELECT COUNT(*) FROM canonical_product_directory candidate
      WHERE candidate.saby_id = NEW.saby_id AND candidate.active) = 1;
  IF mapped_product IS NULL OR mapped_variant IS NULL THEN RETURN NEW; END IF;

  IF NEW.wb_nm_id IS NOT NULL THEN
    UPDATE product_external_ids SET is_primary = FALSE,
      status = CASE WHEN external_id = NEW.wb_nm_id::TEXT THEN 'active' ELSE 'legacy' END,
      updated_at = CURRENT_TIMESTAMP
    WHERE variant_id = mapped_variant AND provider = 'wildberries' AND id_type = 'sku';
    INSERT INTO product_external_ids(product_id, variant_id, provider, id_type,
      external_id, status, is_primary, source, last_seen_at)
    VALUES(mapped_product, mapped_variant, 'wildberries', 'sku',
      NEW.wb_nm_id::TEXT, 'active', TRUE, 'sync', CURRENT_TIMESTAMP)
    ON CONFLICT(provider, id_type, external_id) DO UPDATE SET
      product_id = EXCLUDED.product_id, variant_id = EXCLUDED.variant_id,
      status = 'active', is_primary = TRUE, last_seen_at = CURRENT_TIMESTAMP,
      updated_at = CURRENT_TIMESTAMP;
  END IF;
  IF NULLIF(BTRIM(NEW.wb_vendor_code), '') IS NOT NULL THEN
    UPDATE product_external_ids SET is_primary = FALSE, status = 'legacy', updated_at = CURRENT_TIMESTAMP
    WHERE variant_id = mapped_variant AND provider = 'wildberries'
      AND id_type = 'vendor_code';
    INSERT INTO product_external_ids(product_id, variant_id, provider, id_type,
      external_id, status, is_primary, source, last_seen_at)
    VALUES(mapped_product, mapped_variant, 'wildberries', 'vendor_code',
      BTRIM(NEW.wb_vendor_code), 'active', TRUE, 'sync', CURRENT_TIMESTAMP)
    ON CONFLICT(provider, id_type, external_id) DO UPDATE SET
      product_id = EXCLUDED.product_id, variant_id = EXCLUDED.variant_id,
      status = 'active', is_primary = TRUE, last_seen_at = CURRENT_TIMESTAMP,
      updated_at = CURRENT_TIMESTAMP;
  END IF;
  IF NULLIF(BTRIM(NEW.ozon_offer_id), '') IS NOT NULL THEN
    UPDATE product_external_ids SET is_primary = FALSE, status = 'legacy', updated_at = CURRENT_TIMESTAMP
    WHERE variant_id = mapped_variant AND provider = 'ozon' AND id_type = 'offer_id';
    INSERT INTO product_external_ids(product_id, variant_id, provider, id_type,
      external_id, status, is_primary, source, last_seen_at)
    VALUES(mapped_product, mapped_variant, 'ozon', 'offer_id',
      BTRIM(NEW.ozon_offer_id), 'active', TRUE, 'sync', CURRENT_TIMESTAMP)
    ON CONFLICT(provider, id_type, external_id) DO UPDATE SET
      product_id = EXCLUDED.product_id, variant_id = EXCLUDED.variant_id,
      status = 'active', is_primary = TRUE, last_seen_at = CURRENT_TIMESTAMP,
      updated_at = CURRENT_TIMESTAMP;
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql;
