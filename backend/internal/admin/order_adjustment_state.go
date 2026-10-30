package admin

import (
	"context"
	"fmt"
)

type OrderAdjustmentState struct {
	ID                 int64           `json:"id"`
	OrderNumber        string          `json:"orderNumber"`
	Subtotal           float64         `json:"subtotal"`
	DiscountType       string          `json:"discountType"`
	DiscountValue      float64         `json:"discountValue"`
	DiscountAmount     float64         `json:"discountAmount"`
	DeliveryFee        float64         `json:"deliveryFee"`
	DeliveryPayee      string          `json:"deliveryPayee"`
	DeliveryFeePending bool            `json:"deliveryFeePending"`
	HasPreorder        bool            `json:"hasPreorder"`
	Status             string          `json:"status"`
	DeliveryMethod     string          `json:"deliveryMethod"`
	CDEKTariffCode     *int            `json:"cdekTariffCode,omitempty"`
	CDEKCreateState    string          `json:"cdekCreateState"`
	CDEKCancelState    string          `json:"cdekCancelState"`
	CDEKStatus         string          `json:"cdekStatus"`
	CDEKStatusReason   string          `json:"cdekStatusReason"`
	CDEKLastError      string          `json:"cdekLastError"`
	Items              []OrderItem     `json:"items"`
	ShipmentOffers     []ShipmentOffer `json:"shipmentOffers"`
}

func (repository *PostgresRepository) OrderAdjustment(ctx context.Context, id int64) (OrderAdjustmentState, error) {
	var state OrderAdjustmentState
	if err := repository.pool.QueryRow(ctx, `
		SELECT id,order_number,subtotal::DOUBLE PRECISION,delivery_fee::DOUBLE PRECISION,delivery_payee,
			delivery_fee_pending=1,has_preorder=1,status,delivery_method,cdek_tariff_code,
			cdek_create_state,cdek_cancel_state,cdek_status,cdek_status_reason,cdek_last_error,manager_discount_type,manager_discount_value::double precision,manager_discount_amount::double precision
		FROM orders WHERE id=$1
	`, id).Scan(&state.ID, &state.OrderNumber, &state.Subtotal, &state.DeliveryFee, &state.DeliveryPayee, &state.DeliveryFeePending, &state.HasPreorder, &state.Status, &state.DeliveryMethod, &state.CDEKTariffCode, &state.CDEKCreateState, &state.CDEKCancelState, &state.CDEKStatus, &state.CDEKStatusReason, &state.CDEKLastError, &state.DiscountType, &state.DiscountValue, &state.DiscountAmount); err != nil {
		return state, err
	}
	// Редактор правит SKU, а не карточку товара, поэтому состояние обязано
	// нести sku и название варианта: иначе менеджер не видит, какой размер
	// лежит в заказе, а сохранение уходит без идентичности строки.
	rows, err := repository.pool.Query(ctx, `SELECT oi.id,COALESCE(oi.product_id,0),COALESCE(oi.sku,''),COALESCE(oi.variant_label,''),oi.product_name,oi.unit_price::DOUBLE PRECISION,oi.quantity,COALESCE((oi.variant_snapshot->>'packageLengthCm')::INTEGER, variant_numeric_attribute(oi.variant_id, 'package_length_cm'), 0)::INTEGER,COALESCE((oi.variant_snapshot->>'packageWidthCm')::INTEGER, variant_numeric_attribute(oi.variant_id, 'package_width_cm'), 0)::INTEGER,COALESCE((oi.variant_snapshot->>'packageHeightCm')::INTEGER, variant_numeric_attribute(oi.variant_id, 'package_height_cm'), 0)::INTEGER,COALESCE((oi.variant_snapshot->>'packageWeightGrams')::INTEGER, variant_numeric_attribute(oi.variant_id, 'package_weight_grams'), 0)::INTEGER FROM order_items oi WHERE oi.order_id=$1 ORDER BY oi.id`, id)
	if err != nil {
		return state, fmt.Errorf("query order adjustment lines: %w", err)
	}
	defer rows.Close()
	state.Items = []OrderItem{}
	for rows.Next() {
		var item OrderItem
		if err := rows.Scan(&item.ID, &item.ProductID, &item.SKU, &item.VariantLabel, &item.ProductName, &item.UnitPrice, &item.Quantity, &item.PackageLengthCM, &item.PackageWidthCM, &item.PackageHeightCM, &item.PackageWeightGrams); err != nil {
			return state, err
		}
		state.Items = append(state.Items, item)
	}
	if err := rows.Err(); err != nil {
		return state, err
	}
	state.ShipmentOffers, err = repository.ShipmentOffers(ctx, id)
	return state, err
}
