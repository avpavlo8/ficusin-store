-- Measured packing belongs to this order revision, never to the product card.
CREATE TABLE IF NOT EXISTS order_packing_workflows (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  order_revision INTEGER NOT NULL,
  idempotency_key TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  boxes JSONB NOT NULL,
  tariff_code INTEGER NOT NULL,
  tariff_name TEXT NOT NULL DEFAULT '',
  delivery_fee NUMERIC(12,2) NOT NULL,
  state TEXT NOT NULL DEFAULT 'queued',
  payment_url TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  locked_until TIMESTAMPTZ,
  next_attempt_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(order_id,idempotency_key)
);
CREATE UNIQUE INDEX IF NOT EXISTS order_packing_one_current_idx ON order_packing_workflows(order_id)
  WHERE state NOT IN ('cancelled','stale');
ALTER TABLE outbox ADD COLUMN IF NOT EXISTS order_packing_id BIGINT REFERENCES order_packing_workflows(id) ON DELETE SET NULL;
CREATE UNIQUE INDEX IF NOT EXISTS outbox_one_order_packing_idx ON outbox(order_packing_id) WHERE order_packing_id IS NOT NULL;

-- Any cancellation or later composition change invalidates unsent instructions.
CREATE OR REPLACE FUNCTION invalidate_order_packing_workflow() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.status='cancelled' OR NEW.shipment_revision<>OLD.shipment_revision THEN
    UPDATE order_packing_workflows SET state=CASE WHEN NEW.status='cancelled' THEN 'cancelled' ELSE 'stale' END,updated_at=CURRENT_TIMESTAMP
      WHERE order_id=NEW.id AND state NOT IN ('cancelled','stale');
    UPDATE outbox SET cancelled_at=CURRENT_TIMESTAMP,cancel_reason='order_changed'
      WHERE order_packing_id IN (SELECT id FROM order_packing_workflows WHERE order_id=NEW.id)
        AND sent_at IS NULL AND cancelled_at IS NULL;
  END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS orders_invalidate_packing ON orders;
CREATE TRIGGER orders_invalidate_packing AFTER UPDATE OF status,shipment_revision ON orders FOR EACH ROW EXECUTE FUNCTION invalidate_order_packing_workflow();
