-- Resumable one-off enrichment of the positive-stock Saby import. Drafts stay
-- drafts: AI content is material for the manager, never an automatic publish.
CREATE TABLE IF NOT EXISTS catalog_ai_enrichment_jobs (
  product_id BIGINT PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
  text_status TEXT NOT NULL DEFAULT 'pending' CHECK (text_status IN ('pending','processing','done','failed')),
  image_status TEXT NOT NULL DEFAULT 'pending' CHECK (image_status IN ('pending','processing','done','failed','skipped')),
  text_attempts INTEGER NOT NULL DEFAULT 0,
  image_attempts INTEGER NOT NULL DEFAULT 0,
  cover_prompt TEXT NOT NULL DEFAULT '',
  text_error TEXT NOT NULL DEFAULT '',
  image_error TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO catalog_ai_enrichment_jobs(product_id)
SELECT DISTINCT product.id
FROM products product
JOIN product_variants variant ON variant.product_id=product.id AND variant.is_active
JOIN inventory ON inventory.variant_id=variant.id AND inventory.available_qty>0
WHERE product.status='draft'
  AND EXISTS (
    SELECT 1 FROM product_external_ids external
    WHERE external.product_id=product.id AND external.provider='saby' AND external.id_type='code'
  )
ON CONFLICT(product_id) DO NOTHING;

-- A killed container must not strand work forever.
UPDATE catalog_ai_enrichment_jobs SET text_status='pending'
WHERE text_status='processing';
UPDATE catalog_ai_enrichment_jobs SET image_status='pending'
WHERE image_status='processing';
