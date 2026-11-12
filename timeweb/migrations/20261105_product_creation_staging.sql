-- Reviewable product-creation workflow. No outbound operations are performed
-- by this staging implementation; every simulated remote ID is namespaced STG-.
CREATE TABLE IF NOT EXISTS product_creation_drafts (
  id TEXT PRIMARY KEY,
  customer_id BIGINT NOT NULL REFERENCES customers(id),
  data JSONB NOT NULL DEFAULT '{}'::jsonb,
  revision INTEGER NOT NULL DEFAULT 0,
  product_id BIGINT REFERENCES products(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS product_creation_one_open_per_actor
  ON product_creation_drafts(customer_id) WHERE product_id IS NULL;
CREATE TABLE IF NOT EXISTS product_creation_photos (
  id TEXT PRIMARY KEY,
  draft_id TEXT NOT NULL REFERENCES product_creation_drafts(id) ON DELETE CASCADE,
  filename TEXT NOT NULL,
  image BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS product_creation_photos_draft ON product_creation_photos(draft_id);
CREATE TABLE IF NOT EXISTS product_creation_staging_cards (
  draft_id TEXT NOT NULL REFERENCES product_creation_drafts(id),
  channel TEXT NOT NULL CHECK(channel IN ('saby','wb','ozon')),
  external_id TEXT NOT NULL,
  data JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(draft_id,channel),
  UNIQUE(channel,external_id),
  CHECK(external_id LIKE 'STG-%')
);
