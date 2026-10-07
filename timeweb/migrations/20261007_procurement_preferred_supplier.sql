ALTER TABLE procurement_product_channels
  ADD COLUMN IF NOT EXISTS preferred_supplier_id BIGINT REFERENCES procurement_suppliers(id) ON DELETE SET NULL;
