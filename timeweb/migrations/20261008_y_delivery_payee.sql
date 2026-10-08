-- The shop collects delivery with plants for both existing and new orders.
-- Keep the marker for records created during the carrier-payee draft.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_payee TEXT NOT NULL DEFAULT 'shop';
ALTER TABLE shipment_offers ADD COLUMN IF NOT EXISTS delivery_payee TEXT NOT NULL DEFAULT 'shop';

ALTER TABLE orders ALTER COLUMN delivery_payee SET DEFAULT 'shop';
ALTER TABLE shipment_offers ALTER COLUMN delivery_payee SET DEFAULT 'shop';

ALTER TABLE orders ADD CONSTRAINT orders_delivery_payee_check
  CHECK (delivery_payee IN ('shop', 'carrier'));
ALTER TABLE shipment_offers ADD CONSTRAINT shipment_offers_delivery_payee_check
  CHECK (delivery_payee IN ('shop', 'carrier'));
