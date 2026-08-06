-- What CDEK knows about an order once it has been handed over.
--
-- cdek_uuid is CDEK's own identifier and the only reliable way to ask about
-- a shipment; the tracking number appears a little later, once the parcel
-- is registered, and is what the customer is given.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_uuid TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_track_number TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_status TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_synced_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS orders_cdek_uuid_idx ON orders (cdek_uuid)
  WHERE cdek_uuid <> '';
