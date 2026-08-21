package admin

import (
	"context"
	"fmt"
)

type OrderAdjustmentState struct {
	ID                 int64       `json:"id"`
	OrderNumber        string      `json:"orderNumber"`
	Subtotal           float64     `json:"subtotal"`
	DeliveryFee        float64     `json:"deliveryFee"`
	DeliveryFeePending bool        `json:"deliveryFeePending"`
	HasPreorder        bool        `json:"hasPreorder"`
	Status             string      `json:"status"`
	Items              []OrderItem `json:"items"`
}

func (repository *PostgresRepository) OrderAdjustment(ctx context.Context, id int64) (OrderAdjustmentState, error) {
	var state OrderAdjustmentState
	if err := repository.pool.QueryRow(ctx, `
		SELECT id,order_number,subtotal::DOUBLE PRECISION,delivery_fee::DOUBLE PRECISION,
			delivery_fee_pending=1,has_preorder=1,status FROM orders WHERE id=$1
	`, id).Scan(&state.ID, &state.OrderNumber, &state.Subtotal, &state.DeliveryFee, &state.DeliveryFeePending, &state.HasPreorder, &state.Status); err != nil {
		return state, err
	}
	rows, err := repository.pool.Query(ctx, `
		SELECT product_id,sku,variant_label,product_name,unit_price::DOUBLE PRECISION,quantity
		FROM order_items WHERE order_id=$1 ORDER BY id
	`, id)
	if err != nil {
		return state, fmt.Errorf("query order adjustment lines: %w", err)
	}
	defer rows.Close()
	state.Items = []OrderItem{}
	for rows.Next() {
		var item OrderItem
		if err := rows.Scan(&item.ProductID, &item.SKU, &item.VariantLabel, &item.ProductName, &item.UnitPrice, &item.Quantity); err != nil {
			return state, err
		}
		state.Items = append(state.Items, item)
	}
	return state, rows.Err()
}
