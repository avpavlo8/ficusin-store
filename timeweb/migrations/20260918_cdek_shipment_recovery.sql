-- A CDEK application is not yet a physically shipped parcel. Partial
-- shipments keep the provider's lifecycle and ambiguous-create diagnostic
-- separately from the customer-facing status.
ALTER TABLE shipment_offers
  ADD COLUMN IF NOT EXISTS cdek_status_reason TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS shipment_offers_cdek_recovery_idx
  ON shipment_offers(cdek_next_attempt_at, id)
  WHERE cdek_create_state = 'unknown' AND cdek_uuid = '';

CREATE INDEX IF NOT EXISTS orders_cdek_recovery_idx
  ON orders(cdek_next_attempt_at, id)
  WHERE cdek_create_state = 'unknown' AND cdek_uuid = '';
