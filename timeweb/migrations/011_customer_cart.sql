-- The cart of a signed-in customer, so it survives a new phone or a cleared
-- browser. Guests keep theirs in the browser only.
--
-- Items are stored as JSON rather than rows: a cart is always read and
-- written whole, and this way there is no chance of a half-written cart.
CREATE TABLE IF NOT EXISTS customer_carts (
  customer_id BIGINT PRIMARY KEY REFERENCES customers(id) ON DELETE CASCADE,
  items JSONB NOT NULL DEFAULT '{}'::jsonb,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
