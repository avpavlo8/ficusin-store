-- Popularity reads real order lines by public product slug. This index keeps
-- the catalogue aggregation bounded as order history grows; the partial
-- orders index narrows the join to records that can contribute to the score.
CREATE INDEX IF NOT EXISTS order_items_product_order_idx
  ON order_items (product_id, order_id);

CREATE INDEX IF NOT EXISTS orders_popularity_idx
  ON orders (created_at DESC, id)
  WHERE status <> 'cancelled' AND (status = 'completed' OR payment_status = 'paid');
