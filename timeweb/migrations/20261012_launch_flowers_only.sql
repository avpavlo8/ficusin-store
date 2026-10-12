-- Launch assortment: keep plants visible; the owner will publish other
-- merchandise manually after preparing its cards. Preserve the previous
-- publication state for an auditable, reversible change.
CREATE TABLE IF NOT EXISTS launch_hidden_nonplant_products (
  product_id BIGINT PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
  previous_status TEXT NOT NULL,
  hidden_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO launch_hidden_nonplant_products(product_id, previous_status)
SELECT id, status FROM products
WHERE catalog_section <> 'plants' AND status = 'published'
ON CONFLICT (product_id) DO NOTHING;

UPDATE products
SET status = 'draft',
    override_fields = ARRAY(SELECT DISTINCT unnest(override_fields || ARRAY['status']::TEXT[])),
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT product_id FROM launch_hidden_nonplant_products)
  AND catalog_section <> 'plants' AND status = 'published';
