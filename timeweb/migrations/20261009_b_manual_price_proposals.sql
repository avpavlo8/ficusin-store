ALTER TABLE procurement_action_batches
  ALTER COLUMN procurement_order_id DROP NOT NULL;
ALTER TABLE procurement_action_batches
  ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'procurement',
  ADD COLUMN IF NOT EXISTS manual_variant_id BIGINT REFERENCES product_variants(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS manual_site_price_minor BIGINT;
ALTER TABLE procurement_action_batches
  ADD CONSTRAINT procurement_action_batches_source_check
  CHECK ((source = 'procurement' AND procurement_order_id IS NOT NULL)
      OR (source = 'manual' AND manual_variant_id IS NOT NULL AND manual_site_price_minor IS NOT NULL AND procurement_order_id IS NULL));
CREATE INDEX IF NOT EXISTS procurement_manual_price_batches_variant_idx
  ON procurement_action_batches(manual_variant_id, created_at DESC) WHERE source = 'manual';
ALTER TABLE procurement_action_items
  ADD COLUMN IF NOT EXISTS approved_by BIGINT REFERENCES customers(id) ON DELETE SET NULL;
