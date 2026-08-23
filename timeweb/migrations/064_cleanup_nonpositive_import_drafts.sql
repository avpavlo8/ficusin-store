-- Remove the accidental part of the 2026-08-23 Saby import. These cards were
-- created as drafts, so they never belonged to the public catalogue. Keep all
-- manually created drafts, published/archived cards and anything with order
-- history. Positive stock is summed across every warehouse and SKU.
WITH removable AS (
    SELECT p.id
    FROM products p
    WHERE p.status = 'draft'
      AND NULLIF(BTRIM(p.saby_id), '') IS NOT NULL
      AND NOT EXISTS (
          SELECT 1
          FROM product_variants pv
          JOIN inventory i ON i.variant_id = pv.id
          WHERE pv.product_id = p.id
            AND GREATEST(i.available_qty - i.reserved_qty, 0) > 0
      )
      AND NOT EXISTS (
          SELECT 1
          FROM product_variants pv
          JOIN order_items oi ON oi.variant_id = pv.id
          WHERE pv.product_id = p.id
      )
)
DELETE FROM products p
USING removable r
WHERE p.id = r.id;
