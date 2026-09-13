-- A partial shipment is a durable commercial offer. It snapshots the exact
-- goods, prices, delivery, packing and order revision the customer accepts.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS shipment_revision INTEGER NOT NULL DEFAULT 1;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cancellation_reason TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS shipment_offers (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  public_token TEXT NOT NULL UNIQUE,
  version INTEGER NOT NULL DEFAULT 1,
  order_revision INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'draft'
    CHECK(status IN ('draft','packaging_required','notifying','offered','payment_pending','paid','shipping','shipped','ready','completed','expired','cancelled','stale')),
  delivery_method TEXT NOT NULL,
  address_snapshot TEXT NOT NULL DEFAULT '',
  delivery_fee NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK(delivery_fee >= 0),
  subtotal NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK(subtotal >= 0),
  total NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK(total >= 0),
  cdek_tariff_code INTEGER,
  cdek_tariff_name TEXT NOT NULL DEFAULT '',
  quote_fingerprint TEXT NOT NULL DEFAULT '',
  manager_note TEXT NOT NULL DEFAULT '',
  notified_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  checkout_locked_until TIMESTAMPTZ,
  cdek_uuid TEXT NOT NULL DEFAULT '',
  cdek_track_number TEXT NOT NULL DEFAULT '',
  cdek_status TEXT NOT NULL DEFAULT '',
  cdek_create_state TEXT NOT NULL DEFAULT 'new',
  cdek_attempts INTEGER NOT NULL DEFAULT 0,
  cdek_next_attempt_at TIMESTAMPTZ,
  cdek_last_error TEXT NOT NULL DEFAULT '',
  cdek_synced_at TIMESTAMPTZ,
  created_by BIGINT REFERENCES customers(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS shipment_offers_one_active_idx ON shipment_offers(order_id)
  WHERE status IN ('draft','packaging_required','notifying','offered','payment_pending');
CREATE INDEX IF NOT EXISTS shipment_offers_expiry_idx ON shipment_offers(expires_at,id)
  WHERE status IN ('offered','payment_pending');

CREATE TABLE IF NOT EXISTS shipment_offer_items (
  id BIGSERIAL PRIMARY KEY,
  shipment_offer_id BIGINT NOT NULL REFERENCES shipment_offers(id) ON DELETE CASCADE,
  order_item_id BIGINT NOT NULL REFERENCES order_items(id) ON DELETE CASCADE,
  variant_id BIGINT REFERENCES product_variants(id) ON DELETE RESTRICT,
  sku TEXT NOT NULL DEFAULT '',
  product_name TEXT NOT NULL,
  unit_price NUMERIC(12,2) NOT NULL CHECK(unit_price >= 0),
  quantity INTEGER NOT NULL CHECK(quantity > 0),
  checkout_reserved_qty INTEGER NOT NULL DEFAULT 0 CHECK(checkout_reserved_qty >= 0),
  UNIQUE(shipment_offer_id,order_item_id)
);

CREATE TABLE IF NOT EXISTS shipment_offer_boxes (
  id BIGSERIAL PRIMARY KEY,
  shipment_offer_id BIGINT NOT NULL REFERENCES shipment_offers(id) ON DELETE CASCADE,
  box_no INTEGER NOT NULL CHECK(box_no > 0),
  length_cm INTEGER NOT NULL CHECK(length_cm > 0),
  width_cm INTEGER NOT NULL CHECK(width_cm > 0),
  height_cm INTEGER NOT NULL CHECK(height_cm > 0),
  weight_grams INTEGER NOT NULL CHECK(weight_grams > 0),
  contents JSONB NOT NULL DEFAULT '[]'::JSONB,
  UNIQUE(shipment_offer_id,box_no)
);

-- Exact extra reservations made when a preorder unit becomes available at
-- checkout. Keeping the inventory row makes rollback precise across several
-- warehouses and prevents releasing somebody else's stock.
CREATE TABLE IF NOT EXISTS shipment_offer_inventory_reservations (
  shipment_offer_item_id BIGINT NOT NULL REFERENCES shipment_offer_items(id) ON DELETE CASCADE,
  inventory_id BIGINT NOT NULL REFERENCES inventory(id) ON DELETE RESTRICT,
  quantity INTEGER NOT NULL CHECK(quantity > 0),
  PRIMARY KEY(shipment_offer_item_id,inventory_id)
);

ALTER TABLE payments ADD COLUMN IF NOT EXISTS shipment_offer_id BIGINT REFERENCES shipment_offers(id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX IF NOT EXISTS payments_one_pending_per_offer_idx ON payments(shipment_offer_id)
  WHERE shipment_offer_id IS NOT NULL AND status='pending';

-- A successful notification means the SMTP server accepted the message. It
-- does not claim that the customer opened or read it. The lease prevents two
-- LetterWorker processes from sending the same row concurrently.
ALTER TABLE outbox
  ADD COLUMN IF NOT EXISTS shipment_offer_id BIGINT REFERENCES shipment_offers(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS lock_token TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS outbox_one_shipment_offer_idx ON outbox(shipment_offer_id)
  WHERE shipment_offer_id IS NOT NULL AND cancelled_at IS NULL;
