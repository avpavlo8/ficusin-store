-- Payments taken through YooKassa.
--
-- The row is created before the customer is sent to the payment page, so a
-- payment that is started and never finished is still visible to us. Money
-- that moved without a row here would be money we cannot explain.
CREATE TABLE IF NOT EXISTS payments (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  provider TEXT NOT NULL DEFAULT 'yookassa',
  -- provider_payment_id is empty only in the moment between our INSERT and
  -- YooKassa answering; it is what every later status check goes by.
  provider_payment_id TEXT NOT NULL DEFAULT '',
  -- idempotence_key stops a double-clicked button from creating two
  -- payments: YooKassa returns the original payment for a repeated key.
  idempotence_key TEXT NOT NULL,
  amount NUMERIC(10, 2) NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  confirmation_url TEXT NOT NULL DEFAULT '',
  paid_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS payments_idempotence_key_idx
  ON payments (idempotence_key);
CREATE INDEX IF NOT EXISTS payments_order_idx ON payments (order_id);
CREATE UNIQUE INDEX IF NOT EXISTS payments_provider_payment_idx
  ON payments (provider_payment_id)
  WHERE provider_payment_id <> '';

-- paid_at on the order itself: the shop asks "is this order paid" far more
-- often than it asks anything about the payment attempts behind it.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS paid_at TIMESTAMPTZ;
