-- Preserve the payment contract of historical orders and shipment offers.
-- Before this release the shop charged delivery with plants; new records
-- charge plants only and the carrier collects transport separately.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_payee TEXT NOT NULL DEFAULT 'shop';
ALTER TABLE shipment_offers ADD COLUMN IF NOT EXISTS delivery_payee TEXT NOT NULL DEFAULT 'shop';

ALTER TABLE orders ALTER COLUMN delivery_payee SET DEFAULT 'carrier';
ALTER TABLE shipment_offers ALTER COLUMN delivery_payee SET DEFAULT 'carrier';

ALTER TABLE orders ADD CONSTRAINT orders_delivery_payee_check
  CHECK (delivery_payee IN ('shop', 'carrier'));
ALTER TABLE shipment_offers ADD CONSTRAINT shipment_offers_delivery_payee_check
  CHECK (delivery_payee IN ('shop', 'carrier'));
