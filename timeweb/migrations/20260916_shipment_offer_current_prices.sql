-- A preorder is only an estimate. When plants arrive, a shipment offer
-- snapshots the current storefront price after the customer's current retail
-- discount. Historical offers keep the price that was already proposed.
ALTER TABLE shipment_offer_items
  ADD COLUMN IF NOT EXISTS original_unit_price NUMERIC(12,2);

UPDATE shipment_offer_items
SET original_unit_price = unit_price
WHERE original_unit_price IS NULL;

