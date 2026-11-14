-- Collection start, never upload/commit time: a request fetched before posting
-- must not release a hold just because its delayed upload arrived afterwards.
ALTER TABLE inventory ADD COLUMN stock_observed_at TIMESTAMPTZ;

CREATE VIEW order_sale_stock_holds AS
 SELECT i.id AS intent_id,i.order_id,i.warehouse_id,l.variant_id,l.quantity,
  CASE WHEN COALESCE(p.stock_verified,FALSE) THEN l.quantity ELSE 0 END AS posted_quantity,
  COALESCE(p.execution_mode='live' AND p.stock_verified_at IS NOT NULL
   AND stock.stock_observed_at>p.stock_verified_at,FALSE) AS reflected
 FROM order_sale_intents i JOIN order_sale_intent_lines l ON l.intent_id=i.id
 LEFT JOIN order_sale_publications p ON p.intent_id=i.id
 LEFT JOIN inventory stock ON stock.warehouse_id=i.warehouse_id AND stock.variant_id=l.variant_id
 WHERE i.event_trigger='created' AND NOT i.cancellation_verified
  AND NOT COALESCE(p.cancellation_verified,FALSE);

-- Preserve negative residuals until an owner-specific check adds its credit.
-- Clamping before credit would let a later order borrow the first one's plant.
CREATE VIEW order_sale_inventory AS
 SELECT stock.id,stock.warehouse_id,stock.variant_id,
  GREATEST(-2147483648,LEAST(2147483647,stock.available_qty::BIGINT-COALESCE(held.quantity,0)))::INTEGER AS available_qty,
  stock.reserved_qty,stock.synced_at,stock.stock_observed_at
 FROM inventory stock LEFT JOIN LATERAL (
  SELECT SUM(h.quantity) AS quantity FROM order_sale_stock_holds h
  WHERE h.variant_id=stock.variant_id AND h.warehouse_id=stock.warehouse_id AND NOT h.reflected
 ) held ON TRUE;

-- Credit for checking an existing order, not extra stock for the storefront.
-- Earlier orders have priority over later/preorder holds. A proven posting
-- belongs to this order even while a cached stock import is catching up.
CREATE FUNCTION order_sale_owned_quantity(order_id_arg BIGINT, warehouse_id_arg BIGINT, variant_id_arg BIGINT)
RETURNS INTEGER LANGUAGE SQL STABLE AS $$
 WITH mine AS (
  SELECT MIN(intent_id) AS first_intent,COALESCE(SUM(posted_quantity),0) AS posted
  FROM order_sale_stock_holds
  WHERE order_id=order_id_arg AND warehouse_id=warehouse_id_arg AND variant_id=variant_id_arg
 ), pending AS (
  SELECT COALESCE(SUM(h.quantity) FILTER(WHERE NOT h.reflected),0) AS all_quantity,
   COALESCE(SUM(h.quantity) FILTER(WHERE NOT h.reflected AND h.intent_id<m.first_intent),0) AS earlier_quantity
  FROM order_sale_stock_holds h CROSS JOIN mine m
  WHERE h.warehouse_id=warehouse_id_arg AND h.variant_id=variant_id_arg
 ) SELECT CASE WHEN mine.first_intent IS NULL THEN 0 ELSE
  LEAST(2147483647,GREATEST(0,
   GREATEST(stock.available_qty::BIGINT-stock.reserved_qty-pending.earlier_quantity,mine.posted,0)
   -(GREATEST(-2147483648,LEAST(2147483647,stock.available_qty::BIGINT-pending.all_quantity))-stock.reserved_qty)
  ))::INTEGER END
 FROM inventory stock CROSS JOIN mine CROSS JOIN pending
 WHERE stock.warehouse_id=warehouse_id_arg AND stock.variant_id=variant_id_arg
$$;
