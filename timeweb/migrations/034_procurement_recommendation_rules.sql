-- Supplier packs belong to the supplier/product pair. Recommendations round
-- only after demand, Saby balance and all open procurement orders are netted.
ALTER TABLE procurement_supplier_products
  ADD COLUMN IF NOT EXISTS minimum_order_qty INTEGER NOT NULL DEFAULT 1
    CHECK (minimum_order_qty > 0),
  ADD COLUMN IF NOT EXISTS order_multiple INTEGER NOT NULL DEFAULT 1
    CHECK (order_multiple > 0);

