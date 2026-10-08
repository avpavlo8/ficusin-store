-- Completed orders no longer need a storefront hold. The Saby balance remains
-- the source of physical stock; this migration changes only reserved_qty and
-- records the matching local release movement. It is idempotent via
-- orders.stock_released_at and processes orders in a stable lock order.
DO $$
DECLARE completed_order RECORD;
BEGIN
  FOR completed_order IN
    SELECT id FROM orders
    WHERE status = 'completed' AND stock_released_at IS NULL
    ORDER BY id FOR UPDATE
  LOOP
    PERFORM stock.id FROM inventory stock
    WHERE stock.variant_id IN (
      SELECT variant_id FROM order_items
      WHERE order_id = completed_order.id AND variant_id IS NOT NULL
    )
    ORDER BY stock.id FOR UPDATE;

    WITH taken AS (
      SELECT variant_id, SUM(reserved_qty)::INTEGER AS quantity
      FROM order_items
      WHERE order_id = completed_order.id AND variant_id IS NOT NULL
      GROUP BY variant_id
    ), allocation AS (
      SELECT stock.id, LEAST(
        stock.reserved_qty,
        GREATEST(taken.quantity - COALESCE(SUM(stock.reserved_qty) OVER (
          PARTITION BY stock.variant_id ORDER BY stock.id
          ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
        ), 0), 0)
      ) AS give_back
      FROM inventory stock JOIN taken ON taken.variant_id = stock.variant_id
    )
    UPDATE inventory stock SET reserved_qty = stock.reserved_qty - allocation.give_back
    FROM allocation
    WHERE allocation.id = stock.id AND allocation.give_back > 0;

    INSERT INTO stock_movements (order_id, variant_id, saby_id, kind, quantity, status, reason)
    SELECT item.order_id, item.variant_id, COALESCE(variant.saby_id, ''), 'release',
      SUM(item.reserved_qty)::INTEGER, 'pending', 'completed_order_reconciliation_20261008'
    FROM order_items item
    LEFT JOIN product_variants variant ON variant.id = item.variant_id
    WHERE item.order_id = completed_order.id
      AND item.variant_id IS NOT NULL AND item.reserved_qty > 0
    GROUP BY item.order_id, item.variant_id, variant.saby_id;

    UPDATE orders SET stock_released_at = CURRENT_TIMESTAMP
    WHERE id = completed_order.id;
  END LOOP;
END $$;
