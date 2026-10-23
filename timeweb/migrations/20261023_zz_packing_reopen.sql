-- Keep carrier attempts distinct when a prepared order is revised.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_order_number TEXT NOT NULL DEFAULT '';
