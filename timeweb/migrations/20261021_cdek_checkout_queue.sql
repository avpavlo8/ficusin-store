-- Durable opt-in captured at checkout. Do not backfill abandoned unpaid orders.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_checkout_requested SMALLINT NOT NULL DEFAULT 0 CHECK (cdek_checkout_requested IN (0,1));
