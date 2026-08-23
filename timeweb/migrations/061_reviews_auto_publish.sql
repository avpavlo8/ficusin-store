-- Reviews from verified completed purchases are published immediately.
-- Keep at most one visible review per customer and product. Historical
-- duplicates are preserved as rejected records instead of being deleted.
WITH ranked AS (
  SELECT id,ROW_NUMBER() OVER (
    PARTITION BY product_id,customer_id ORDER BY created_at DESC,id DESC
  ) AS position
  FROM product_reviews
  WHERE status IN ('pending','published')
)
UPDATE product_reviews review
SET status='rejected',moderated_at=COALESCE(review.moderated_at,CURRENT_TIMESTAMP)
FROM ranked
WHERE review.id=ranked.id AND ranked.position>1;

UPDATE product_reviews
SET status='published',moderated_at=NULL,moderated_by=NULL
WHERE status='pending';

ALTER TABLE product_reviews ALTER COLUMN status SET DEFAULT 'published';

CREATE UNIQUE INDEX IF NOT EXISTS product_reviews_customer_product_active_uidx
  ON product_reviews(product_id,customer_id)
  WHERE status IN ('pending','published');
