-- First-party product analytics. External counters are useful for acquisition,
-- but the store must retain its own canonical funnel and order attribution.
CREATE TABLE IF NOT EXISTS analytics_events (
  id BIGSERIAL PRIMARY KEY,
  event_id UUID NOT NULL UNIQUE,
  event_name TEXT NOT NULL,
  visitor_id UUID NOT NULL,
  session_id UUID NOT NULL,
  customer_id BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  occurred_at TIMESTAMPTZ NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  page_path TEXT NOT NULL DEFAULT '',
  page_title TEXT NOT NULL DEFAULT '',
  referrer TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT '',
  medium TEXT NOT NULL DEFAULT '',
  campaign TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  term TEXT NOT NULL DEFAULT '',
  product_code TEXT NOT NULL DEFAULT '',
  sku TEXT NOT NULL DEFAULT '',
  order_number TEXT NOT NULL DEFAULT '',
  value NUMERIC(14,2) NOT NULL DEFAULT 0,
  quantity INTEGER NOT NULL DEFAULT 0,
  properties JSONB NOT NULL DEFAULT '{}'::JSONB,
  trusted SMALLINT NOT NULL DEFAULT 0 CHECK (trusted IN (0,1))
);

CREATE INDEX IF NOT EXISTS analytics_events_occurred_idx
  ON analytics_events (occurred_at DESC);
CREATE INDEX IF NOT EXISTS analytics_events_funnel_idx
  ON analytics_events (event_name, occurred_at DESC);
CREATE INDEX IF NOT EXISTS analytics_events_session_idx
  ON analytics_events (session_id, occurred_at);
CREATE INDEX IF NOT EXISTS analytics_events_product_idx
  ON analytics_events (product_code, event_name, occurred_at DESC)
  WHERE product_code <> '';
CREATE INDEX IF NOT EXISTS analytics_events_order_idx
  ON analytics_events (order_number)
  WHERE order_number <> '';

-- Orders keep the acquisition snapshot that was active when checkout began.
-- This remains useful even after raw analytics events reach their retention age.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS analytics_visitor_id UUID;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS analytics_session_id UUID;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS attribution JSONB NOT NULL DEFAULT '{}'::JSONB;

