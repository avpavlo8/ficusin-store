-- Records that a person agreed to the privacy policy and the offer.
-- Kept separate from customers because a guest checkout has no customer row,
-- and because consent has to stay on file even if the account is deleted.
CREATE TABLE IF NOT EXISTS consent_events (
  id BIGSERIAL PRIMARY KEY,
  customer_id BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  order_id BIGINT REFERENCES orders(id) ON DELETE SET NULL,
  event TEXT NOT NULL CHECK (event IN ('registration', 'order')),
  documents TEXT NOT NULL,
  document_version TEXT NOT NULL,
  phone TEXT NOT NULL DEFAULT '',
  ip_address TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS consent_events_customer_idx
  ON consent_events (customer_id, created_at DESC);
CREATE INDEX IF NOT EXISTS consent_events_order_idx
  ON consent_events (order_id);
CREATE INDEX IF NOT EXISTS consent_events_phone_idx
  ON consent_events (phone, created_at DESC);

-- Which variant an order line took stock from, so a cancelled order can
-- give the reservation back.
ALTER TABLE order_items
  ADD COLUMN IF NOT EXISTS variant_id BIGINT REFERENCES product_variants(id) ON DELETE SET NULL;

ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS stock_released_at TIMESTAMPTZ;

-- Admin rights used to be granted by matching an email address, which
-- nobody ever verifies. Bind every existing grant to a concrete account and
-- switch off the ones that point at an address with no account behind it.
UPDATE admin_users au
  SET customer_id = c.id, updated_at = CURRENT_TIMESTAMP
  FROM customers c
  WHERE au.customer_id IS NULL
    AND au.email IS NOT NULL
    AND c.email IS NOT NULL
    AND LOWER(c.email) = LOWER(au.email)
    AND NOT EXISTS (
      SELECT 1 FROM admin_users other WHERE other.customer_id = c.id
    );

-- What is left points at an address nobody has registered. Such a row grants
-- nothing now, and keeping it would hold the address hostage against the
-- UNIQUE index on admin_users.email.
DELETE FROM admin_users WHERE customer_id IS NULL;
