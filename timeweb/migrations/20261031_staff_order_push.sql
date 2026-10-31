-- Only newly committed checkout orders enqueue notifications; no historical backfill.
CREATE TABLE staff_order_push_outbox (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  subscription_id BIGINT NOT NULL REFERENCES push_subscriptions(id) ON DELETE CASCADE,
  customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  attempts INTEGER NOT NULL DEFAULT 0,
  completed_at TIMESTAMPTZ,
  outcome TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  UNIQUE(order_id, subscription_id)
);
CREATE INDEX staff_order_push_pending_idx ON staff_order_push_outbox(next_attempt_at,id)
  WHERE completed_at IS NULL;
