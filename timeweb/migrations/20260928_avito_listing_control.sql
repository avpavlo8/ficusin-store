CREATE TABLE IF NOT EXISTS avito_listings (
  item_id TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL DEFAULT '',
  remote_price_minor BIGINT NOT NULL DEFAULT 0,
  desired_published BOOLEAN,
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_reconciled_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS avito_listing_products (
  item_id TEXT NOT NULL REFERENCES avito_listings(item_id) ON DELETE CASCADE,
  product_id BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (item_id, product_id)
);

CREATE INDEX IF NOT EXISTS avito_listing_products_product_idx
  ON avito_listing_products(product_id);

CREATE TABLE IF NOT EXISTS avito_integration_state (
  id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  publication_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  feed_token TEXT NOT NULL DEFAULT (md5(random()::TEXT || clock_timestamp()::TEXT) || md5(random()::TEXT)),
  last_import_at TIMESTAMPTZ,
  last_worker_at TIMESTAMPTZ,
  last_success_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT ''
);
INSERT INTO avito_integration_state(id) VALUES(1) ON CONFLICT (id) DO NOTHING;

ALTER TABLE procurement_action_items
  DROP CONSTRAINT IF EXISTS procurement_action_items_channel_check;
ALTER TABLE procurement_action_items
  ADD CONSTRAINT procurement_action_items_channel_check
  CHECK (channel IN ('saby_receipt', 'site', 'saby_price', 'wb', 'ozon', 'avito'));
