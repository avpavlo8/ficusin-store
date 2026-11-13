package admin

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// A creation-time realization freezes item identity, quantity and price. Until
// a controlled adjustment protocol exists, changing those terms requires a
// cancelled order and a new purchase. Re-submitting unchanged form fields is
// still allowed and must not delete/recreate the frozen order-item IDs.
func guardCreationRealizationEdit(ctx context.Context, tx pgx.Tx, orderID int64, edit *OrderEdit) error {
	var frozen bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM order_sale_intents WHERE order_id=$1 AND event_trigger='created')`, orderID).Scan(&frozen); err != nil {
		return err
	}
	if !frozen {
		return nil
	}
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
			if current[line.identity()] != line.Quantity {
				unchanged = false
			}
		}
		if !unchanged {
			return fmt.Errorf("%w: состав уже зафиксирован для списания СБИС; отмените заказ и оформите новый, чтобы изменить товары или количество", ErrInvalidInput)
		}
		edit.Items = nil
	}
	if edit.Discount != nil {
		var original OrderDiscount
		if err := tx.QueryRow(ctx, `SELECT manager_discount_type,manager_discount_value::DOUBLE PRECISION FROM orders WHERE id=$1`, orderID).Scan(&original.Type, &original.Value); err != nil {
			return err
		}
		if original.Type != edit.Discount.Type || original.Value != edit.Discount.Value {
			return fmt.Errorf("%w: стоимость товаров уже зафиксирована для списания СБИС; отмените заказ и оформите новый, чтобы изменить скидку", ErrInvalidInput)
		}
	}
	return nil
}
