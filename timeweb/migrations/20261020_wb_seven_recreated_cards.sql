-- Owner-authorized repair of the seven WB exceptions from shipment 8208515.
-- These nmIDs and seller barcodes were read back from the seller's own WB
-- catalogue on 20 October. Cards, photos, agreed prices and FBS stock already
-- exist on WB; this migration only fixes their local integration identities.
-- Keep old IDs as legacy so historical sales retain their original mappings.
DO $$
DECLARE
  target RECORD;
  matched INTEGER;
BEGIN
  CREATE TEMP TABLE wb_seven_repair_targets ON COMMIT DROP AS
  SELECT seed.*, directory.variant_id, directory.saby_id
  FROM (VALUES
    ('1',   1::BIGINT,     'X1150532',   1106114508::BIGINT, 1938144313::BIGINT, 'аглаонема в асс. д12', '2059164903397'),
    ('87',  87::BIGINT,    'X315119260', 378558302::BIGINT,  1938104836::BIGINT, 'мухоловка',            '2059164194825'),
    ('145', 145::BIGINT,   'X933312684', 262766221::BIGINT,  1938165260::BIGINT, 'мирт д12',             '2059165119896'),
    ('231', 230::BIGINT,   'X7774683',   964191777::BIGINT,  1938167127::BIGINT, 'хедера д13',           '2059165142542'),
    ('872', 11407::BIGINT, 'X615770892', NULL::BIGINT,       1938131717::BIGINT, 'каламондин д9',        '2059164648052'),
    ('873', 11408::BIGINT, 'X525558640', NULL::BIGINT,       1938136516::BIGINT, 'лайм д9',              '2059164708046'),
    ('874', 11409::BIGINT, 'X740063870', NULL::BIGINT,       1938138261::BIGINT, 'лимон д9',             '2059164815454')
  ) seed(sku, product_id, saby_code, old_nm_id, nm_id, vendor_code, barcode)
  JOIN canonical_product_directory directory
    ON directory.internal_sku=seed.sku AND directory.product_id=seed.product_id
    AND directory.master_code=seed.saby_code AND directory.active;

  SELECT COUNT(*) INTO matched FROM wb_seven_repair_targets;
  -- Empty/independent CI databases have different catalogue identities.
  IF matched=0 THEN RETURN; END IF;
  IF matched<>7 OR (SELECT COUNT(DISTINCT saby_id) FROM wb_seven_repair_targets)<>7 THEN
    RAISE EXCEPTION 'WB seven-card repair: incomplete or ambiguous catalogue identities';
  END IF;
  IF EXISTS (
    SELECT 1 FROM wb_seven_repair_targets t
    JOIN product_external_ids e ON e.provider='wildberries'
      AND ((e.id_type IN ('sku','nm_id') AND e.external_id=t.nm_id::TEXT)
        OR (e.id_type='vendor_code' AND e.external_id=t.vendor_code))
    WHERE e.variant_id IS DISTINCT FROM t.variant_id OR e.product_id<>t.product_id
  ) OR EXISTS (
    SELECT 1 FROM wb_seven_repair_targets t
    JOIN product_external_ids e ON e.variant_id=t.variant_id
      AND e.provider='wildberries' AND e.id_type IN ('sku','nm_id') AND e.status='active'
    WHERE e.external_id IS DISTINCT FROM t.old_nm_id::TEXT AND e.external_id<>t.nm_id::TEXT
  ) THEN
    RAISE EXCEPTION 'WB seven-card repair: unexpected current mapping; manual review required';
  END IF;

  FOR target IN SELECT * FROM wb_seven_repair_targets LOOP
    -- The existing variant-aware trigger updates the canonical WB mapping.
    INSERT INTO procurement_product_channels(saby_id,wb_nm_id,wb_vendor_code)
    VALUES(target.saby_id,target.nm_id,target.vendor_code)
    ON CONFLICT(saby_id) DO UPDATE SET wb_nm_id=EXCLUDED.wb_nm_id,
      wb_vendor_code=EXCLUDED.wb_vendor_code,updated_at=CURRENT_TIMESTAMP;

    -- Also retire any historical alternate technical-ID representation.
    UPDATE product_external_ids SET status='legacy',is_primary=FALSE,updated_at=CURRENT_TIMESTAMP
    WHERE variant_id=target.variant_id AND provider='wildberries'
      AND id_type IN ('sku','nm_id') AND external_id<>target.nm_id::TEXT;
    UPDATE product_external_ids SET metadata=metadata || jsonb_build_object(
      'repair','shipment-8208515-wb-seven-20261020','verified_seller_barcode',target.barcode),
      updated_at=CURRENT_TIMESTAMP
    WHERE variant_id=target.variant_id AND provider='wildberries'
      AND id_type='sku' AND external_id=target.nm_id::TEXT;
  END LOOP;
END $$;
