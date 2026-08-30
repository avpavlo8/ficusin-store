-- In Saby, nomNumber/code (for example X2413914) is a human-readable code,
-- while catalogue.id (for example 2971) is the stable card identifier used
-- by balances and warehouse document rows. During the identity transition
-- procurement could retain both as saby_id, producing duplicate products,
-- zero balances and receipts that could not be created.
--
-- Repair only unambiguous pairs: one historical X row and exactly one active
-- numeric row with the same Saby code. Historical nomenclature itself stays in
-- place; only local procurement references move to the current card.

CREATE TEMP TABLE procurement_saby_id_repairs ON COMMIT DROP AS
WITH active_codes AS (
  SELECT UPPER(BTRIM(code)) AS code_key, MIN(saby_id) AS current_saby_id
  FROM saby_nomenclature
  WHERE missing_since IS NULL
    AND saby_id ~ '^[0-9]+$'
    AND NULLIF(BTRIM(code), '') IS NOT NULL
  GROUP BY UPPER(BTRIM(code))
  HAVING COUNT(*) = 1
)
SELECT old.saby_id AS old_saby_id, active.current_saby_id
FROM saby_nomenclature old
JOIN active_codes active ON active.code_key = UPPER(BTRIM(old.code))
WHERE old.saby_id ~ '^X[0-9]+$'
  AND UPPER(BTRIM(old.saby_id)) = UPPER(BTRIM(old.code))
  AND old.saby_id <> active.current_saby_id;

CREATE TEMP TABLE procurement_repaired_orders ON COMMIT DROP AS
SELECT DISTINCT line.procurement_order_id AS id
FROM procurement_order_lines line
JOIN procurement_saby_id_repairs repair ON repair.old_saby_id = line.saby_id
JOIN procurement_orders orders ON orders.id = line.procurement_order_id
WHERE orders.status NOT IN ('received', 'cancelled');

-- Merge supplier settings before moving aliases and order lines. If the new
-- card already exists, keep its explicit settings and only fill empty values
-- from the historical card.
INSERT INTO procurement_supplier_products (
  supplier_id, saby_id, supplier_article, availability_status,
  unavailable_since, check_after, updated_by, updated_at,
  minimum_order_qty, order_multiple
)
SELECT old.supplier_id, repair.current_saby_id, old.supplier_article,
  old.availability_status, old.unavailable_since, old.check_after,
  old.updated_by, CURRENT_TIMESTAMP, old.minimum_order_qty, old.order_multiple
FROM procurement_supplier_products old
JOIN procurement_saby_id_repairs repair ON repair.old_saby_id = old.saby_id
ON CONFLICT (supplier_id, saby_id) DO UPDATE SET
  supplier_article = COALESCE(NULLIF(procurement_supplier_products.supplier_article, ''), EXCLUDED.supplier_article),
  availability_status = CASE
    WHEN procurement_supplier_products.availability_status = 'unknown' THEN EXCLUDED.availability_status
    ELSE procurement_supplier_products.availability_status
  END,
  unavailable_since = COALESCE(procurement_supplier_products.unavailable_since, EXCLUDED.unavailable_since),
  check_after = COALESCE(procurement_supplier_products.check_after, EXCLUDED.check_after),
  minimum_order_qty = GREATEST(procurement_supplier_products.minimum_order_qty, EXCLUDED.minimum_order_qty),
  order_multiple = GREATEST(procurement_supplier_products.order_multiple, EXCLUDED.order_multiple),
  updated_at = CURRENT_TIMESTAMP;

-- Marketplace IDs were often entered on the old X card. Preserve them while
-- consolidating the row onto the numeric Saby card.
INSERT INTO procurement_product_channels (
  saby_id, holland_article, wb_article, ozon_article,
  updated_by, updated_at, wb_nm_id, wb_vendor_code, ozon_offer_id
)
SELECT repair.current_saby_id, old.holland_article, old.wb_article,
  old.ozon_article, old.updated_by, CURRENT_TIMESTAMP, old.wb_nm_id,
  old.wb_vendor_code, old.ozon_offer_id
FROM procurement_product_channels old
JOIN procurement_saby_id_repairs repair ON repair.old_saby_id = old.saby_id
ON CONFLICT (saby_id) DO UPDATE SET
  holland_article = COALESCE(NULLIF(procurement_product_channels.holland_article, ''), EXCLUDED.holland_article),
  wb_article = COALESCE(NULLIF(procurement_product_channels.wb_article, ''), EXCLUDED.wb_article),
  ozon_article = COALESCE(NULLIF(procurement_product_channels.ozon_article, ''), EXCLUDED.ozon_article),
  wb_nm_id = COALESCE(procurement_product_channels.wb_nm_id, EXCLUDED.wb_nm_id),
  wb_vendor_code = COALESCE(NULLIF(procurement_product_channels.wb_vendor_code, ''), EXCLUDED.wb_vendor_code),
  ozon_offer_id = COALESCE(NULLIF(procurement_product_channels.ozon_offer_id, ''), EXCLUDED.ozon_offer_id),
  updated_at = CURRENT_TIMESTAMP;

INSERT INTO procurement_excluded_products (saby_id, reason, excluded_at, updated_by)
SELECT repair.current_saby_id, old.reason, old.excluded_at, old.updated_by
FROM procurement_excluded_products old
JOIN procurement_saby_id_repairs repair ON repair.old_saby_id = old.saby_id
ON CONFLICT (saby_id) DO UPDATE SET
  reason = COALESCE(NULLIF(procurement_excluded_products.reason, ''), EXCLUDED.reason),
  excluded_at = LEAST(procurement_excluded_products.excluded_at, EXCLUDED.excluded_at);

UPDATE procurement_supplier_aliases alias SET
  matched_saby_id = repair.current_saby_id,
  updated_at = CURRENT_TIMESTAMP
FROM procurement_saby_id_repairs repair
WHERE alias.matched_saby_id = repair.old_saby_id;

UPDATE procurement_order_lines line SET
  saby_id = repair.current_saby_id,
  purchase_unit_rub = NULL,
  trolley_delivery_unit_rub = NULL,
  ryazan_delivery_unit_rub = NULL,
  unit_cost_rub = NULL,
  proposed_retail_rub = NULL,
  proposed_marketplace_rub = NULL,
  proposed_marketplace_strike_rub = NULL,
  updated_at = CURRENT_TIMESTAMP
FROM procurement_saby_id_repairs repair
WHERE line.saby_id = repair.old_saby_id
  AND line.procurement_order_id IN (SELECT id FROM procurement_repaired_orders);

UPDATE procurement_requests request SET saby_id = repair.current_saby_id,
  updated_at = CURRENT_TIMESTAMP
FROM procurement_saby_id_repairs repair
WHERE request.saby_id = repair.old_saby_id;

UPDATE procurement_sales_daily sale SET saby_id = repair.current_saby_id,
  synced_at = CURRENT_TIMESTAMP
FROM procurement_saby_id_repairs repair
WHERE sale.saby_id = repair.old_saby_id;

-- Prepared batches contain immutable snapshots with the obsolete ID. Cancel
-- only those that have not completed an external action, then force a fresh
-- calculation. A conducted/created document is never altered here.
UPDATE procurement_action_batches batch SET status = 'cancelled',
  updated_at = CURRENT_TIMESTAMP
WHERE batch.procurement_order_id IN (SELECT id FROM procurement_repaired_orders)
  AND batch.status IN ('draft', 'approved', 'processing', 'partially_completed', 'failed')
  AND NOT EXISTS (
    SELECT 1 FROM procurement_action_items item
    WHERE item.batch_id = batch.id AND item.status = 'completed'
  );

UPDATE procurement_orders orders SET status = 'review', calculated_at = NULL,
  calculation_settings = NULL, calculation_version = NULL,
  updated_at = CURRENT_TIMESTAMP
WHERE orders.id IN (SELECT id FROM procurement_repaired_orders);

DELETE FROM procurement_supplier_products old
USING procurement_saby_id_repairs repair
WHERE old.saby_id = repair.old_saby_id;

DELETE FROM procurement_product_channels old
USING procurement_saby_id_repairs repair
WHERE old.saby_id = repair.old_saby_id;

DELETE FROM procurement_excluded_products old
USING procurement_saby_id_repairs repair
WHERE old.saby_id = repair.old_saby_id;
