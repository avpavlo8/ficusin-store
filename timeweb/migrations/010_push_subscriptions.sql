-- Browsers that agreed to receive notifications. A subscription belongs to a
-- browser, not to a person: the same customer on a phone and a laptop is two
-- rows, and a guest who has not signed in still gets one.
CREATE TABLE IF NOT EXISTS push_subscriptions (
  id BIGSERIAL PRIMARY KEY,
  customer_id BIGINT REFERENCES customers(id) ON DELETE CASCADE,
  endpoint TEXT NOT NULL UNIQUE,
  p256dh TEXT NOT NULL,
  auth TEXT NOT NULL,
  user_agent TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_sent_at TIMESTAMPTZ,
  -- Set when the push service says the subscription is gone (404/410), so a
  -- dead endpoint is not retried forever.
  expired_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS push_subscriptions_customer_idx
  ON push_subscriptions (customer_id);
CREATE INDEX IF NOT EXISTS push_subscriptions_live_idx
  ON push_subscriptions (expired_at) WHERE expired_at IS NULL;
