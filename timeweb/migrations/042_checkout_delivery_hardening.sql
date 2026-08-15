-- One order must never have two payment pages that can both charge a buyer.
-- Keep the newest historical pending attempt active and close older leftovers
-- before adding the invariant, so the migration is safe on an existing store.
WITH ranked AS (
  SELECT id, ROW_NUMBER() OVER (PARTITION BY order_id ORDER BY id DESC) AS position
  FROM payments
  WHERE status = 'pending'
)
UPDATE payments
SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT id FROM ranked WHERE position > 1);

CREATE UNIQUE INDEX IF NOT EXISTS payments_one_pending_per_order_idx
  ON payments (order_id) WHERE status = 'pending';

-- Provider identifiers are identities, not ordinary attributes. Enforce this
-- in PostgreSQL as well as in worker code.
CREATE UNIQUE INDEX IF NOT EXISTS orders_cdek_uuid_unique_idx
  ON orders (cdek_uuid) WHERE cdek_uuid <> '';

ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_create_state TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_next_attempt_at TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_status_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_cancel_state TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_cancel_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_cancel_next_attempt_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS orders_cdek_retry_idx
  ON orders (cdek_next_attempt_at, id)
  WHERE delivery_method = 'cdek' AND cdek_uuid = '';

CREATE INDEX IF NOT EXISTS orders_cdek_cancel_idx
  ON orders (cdek_cancel_next_attempt_at, id)
  WHERE status = 'cancelled' AND cdek_uuid <> '';
