-- Stock reservation was introduced by commit 2a6cf31 on 2026-08-03T19:13:43Z.
-- Orders created before that instant could not have reserved inventory.
--
-- Migration 051 later had to backfill order_items.reserved_qty without a
-- historical reservation ledger. For ordinary legacy rows it conservatively
-- copied quantity into reserved_qty. That was safe for cancellation (it avoids
-- giving stock back twice), but it also assigned phantom reservations to
-- orders that predate the reservation feature itself. The operations probe now
-- correctly detects those rows as reservation_ledger_mismatch.
--
-- This repair only touches the period for which the answer is certain: before
-- reservation code existed. It does NOT infer or release reservations from the
-- later 2026-08-03..2026-08-16 legacy window, where partial preorder reserves
-- cannot be reconstructed exactly from order history.

DO $$
BEGIN
  WITH pre_reservation_orders AS (
    SELECT id
    FROM orders
    WHERE created_at < TIMESTAMPTZ '2026-08-03 19:13:43+00'
  )
  UPDATE order_items item
  SET reserved_qty = 0
  FROM pre_reservation_orders legacy
  WHERE item.order_id = legacy.id
    AND item.reserved_qty <> 0;

  -- These orders never owned an inventory reservation, so mark the reservation
  -- lifecycle as already closed. Using created_at (rather than migration time)
  -- records the truthful semantic: there was no active reservation at any point.
  UPDATE orders
  SET stock_released_at = created_at
  WHERE created_at < TIMESTAMPTZ '2026-08-03 19:13:43+00'
    AND stock_released_at IS NULL;
END
$$;
