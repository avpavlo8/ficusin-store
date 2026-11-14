package admin

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Filter unchanged form fields before any mutation. Retained line identities
// must not be recreated just because the editor re-submitted its current state.
func filterUnchangedOrderEditFields(ctx context.Context, tx pgx.Tx, orderID int64, edit *OrderEdit) error {
	if edit.Items != nil {
		requested, err := normalizeOrderEditLines(*edit.Items)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT COALESCE(sku,''),SUM(quantity)::INTEGER FROM order_items WHERE order_id=$1 GROUP BY sku`, orderID)
		if err != nil {
			return err
		}
		current := map[string]int{}
		for rows.Next() {
			var sku string
			var quantity int
			if err = rows.Scan(&sku, &quantity); err != nil {
				rows.Close()
				return err
			}
			current[sku] = quantity
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		unchanged := len(requested) == len(current)
		for _, line := range requested {
			if current[line.SKU] != line.Quantity {
				unchanged = false
			}
		}
		if unchanged {
			edit.Items = nil
		}
	}
	if edit.Discount != nil {
		var original OrderDiscount
		if err := tx.QueryRow(ctx, `SELECT manager_discount_type,manager_discount_value::DOUBLE PRECISION FROM orders WHERE id=$1`, orderID).Scan(&original.Type, &original.Value); err != nil {
			return err
		}
		if original.Type == edit.Discount.Type && original.Value == edit.Discount.Value {
			edit.Discount = nil
		}
	}
	return nil
}
