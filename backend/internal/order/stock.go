package order

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ReleaseStock puts an order's reservation back on the shelf. It is safe
// to call twice: orders.stock_released_at is the guard, so a cancelled
// order cannot give the same plants back a second time.
func ReleaseStock(ctx context.Context, tx pgx.Tx, orderID int64) error {
	var alreadyReleased bool
	if err := tx.QueryRow(ctx, `
		SELECT stock_released_at IS NOT NULL FROM orders WHERE id = $1
	`, orderID).Scan(&alreadyReleased); err != nil {
		return fmt.Errorf("read order stock release: %w", err)
	}
	if alreadyReleased {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		SELECT i.id FROM inventory i
		WHERE i.variant_id IN (
			SELECT variant_id FROM order_items
			WHERE order_id = $1 AND variant_id IS NOT NULL
		)
		ORDER BY i.id
		FOR UPDATE
	`, orderID); err != nil {
		return fmt.Errorf("lock inventory: %w", err)
	}
	// A variant may be stocked in several warehouses, so the quantity is
	// given back row by row: each row returns at most what it still holds
	// reserved, and only until the order's quantity is covered.
	if _, err := tx.Exec(ctx, `
		WITH taken AS (
			SELECT variant_id, SUM(quantity)::INTEGER AS quantity
			FROM order_items
			WHERE order_id = $1 AND variant_id IS NOT NULL
			GROUP BY variant_id
		), allocation AS (
			SELECT i.id, LEAST(
				i.reserved_qty,
				GREATEST(t.quantity - COALESCE(SUM(i.reserved_qty) OVER (
					PARTITION BY i.variant_id ORDER BY i.id
					ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
				), 0), 0)
			) AS give_back
			FROM inventory i
			JOIN taken t ON t.variant_id = i.variant_id
		)
		UPDATE inventory i SET reserved_qty = i.reserved_qty - a.give_back
		FROM allocation a
		WHERE a.id = i.id AND a.give_back > 0
	`, orderID); err != nil {
		return fmt.Errorf("release inventory: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE orders SET stock_released_at = CURRENT_TIMESTAMP WHERE id = $1
	`, orderID); err != nil {
		return fmt.Errorf("mark stock released: %w", err)
	}
	return nil
}
