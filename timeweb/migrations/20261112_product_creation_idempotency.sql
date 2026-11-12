-- Creation receipts share the product transaction, including ambiguous commits.
-- Keep a tombstone after deletion so retrying an old draft cannot recreate it.
CREATE TABLE IF NOT EXISTS catalog_product_creation_requests (
 actor_customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
 creation_id TEXT NOT NULL CHECK(creation_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
 product_id BIGINT REFERENCES products(id) ON DELETE SET NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(actor_customer_id,creation_id)
);
CREATE INDEX IF NOT EXISTS catalog_product_creation_requests_product ON catalog_product_creation_requests(product_id) WHERE product_id IS NOT NULL;
