-- Safety and directory layer for procurement. Supplier availability belongs to
-- a canonical supplier/product pair, not to one spelling found in a PDF.
CREATE TABLE IF NOT EXISTS procurement_supplier_products (
  supplier_id BIGINT NOT NULL REFERENCES procurement_suppliers(id) ON DELETE CASCADE,
  saby_id TEXT NOT NULL REFERENCES saby_nomenclature(saby_id) ON DELETE CASCADE,
  supplier_article TEXT NOT NULL DEFAULT '',
  availability_status TEXT NOT NULL DEFAULT 'unknown'
    CHECK (availability_status IN ('available', 'unknown', 'check', 'temporarily_unavailable', 'discontinued')),
  unavailable_since DATE,
  check_after DATE,
  updated_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (supplier_id, saby_id)
);
CREATE INDEX IF NOT EXISTS procurement_supplier_products_availability_idx
  ON procurement_supplier_products (supplier_id, availability_status, check_after);

INSERT INTO procurement_supplier_products (
  supplier_id, saby_id, supplier_article, availability_status,
  unavailable_since, check_after, updated_at
)
SELECT DISTINCT ON (supplier_id, matched_saby_id)
  supplier_id, matched_saby_id, supplier_article, availability_status,
  unavailable_since, check_after, updated_at
FROM procurement_supplier_aliases
WHERE matched_saby_id IS NOT NULL
ORDER BY supplier_id, matched_saby_id, last_seen_at DESC NULLS LAST, id DESC
ON CONFLICT (supplier_id, saby_id) DO NOTHING;

ALTER TABLE procurement_product_channels
  ADD COLUMN IF NOT EXISTS wb_nm_id BIGINT,
  ADD COLUMN IF NOT EXISTS wb_vendor_code TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS ozon_offer_id TEXT NOT NULL DEFAULT '';

UPDATE procurement_product_channels
SET wb_nm_id = wb_article::BIGINT
WHERE wb_nm_id IS NULL AND wb_article ~ '^[0-9]+$';
UPDATE procurement_product_channels
SET ozon_offer_id = ozon_article
WHERE ozon_offer_id = '' AND ozon_article <> '';

ALTER TABLE procurement_order_lines
  ADD COLUMN IF NOT EXISTS comparison_accepted BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS comparison_note TEXT NOT NULL DEFAULT '';

ALTER TABLE procurement_orders
  ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS trolley_cost_rub NUMERIC(14,2) NOT NULL DEFAULT 0;

ALTER TABLE procurement_pricing_settings
  ADD COLUMN IF NOT EXISTS trolley_cost_rub NUMERIC(14,2) NOT NULL DEFAULT 0;

UPDATE procurement_pricing_settings SET trolley_cost_rub = trolley_cost_currency
WHERE trolley_cost_rub = 0 AND trolley_cost_currency > 0;
UPDATE procurement_orders SET trolley_cost_rub = trolley_cost_currency
WHERE trolley_cost_rub = 0 AND trolley_cost_currency > 0;

-- A batch is a snapshot of one calculation. Recalculation cancels drafts and
-- creates a new snapshot instead of mixing old approved items with new rows.
ALTER TABLE procurement_action_batches
  ADD COLUMN IF NOT EXISTS calculation_version INTEGER,
  ADD COLUMN IF NOT EXISTS calculated_at TIMESTAMPTZ;
