package operations

import (
	"context"
	"fmt"
	"time"
)

// Diagnostics is available only through the authenticated admin endpoint.
// It intentionally does not change reservations or retry provider actions.
type Diagnostics struct {
	Overreserved   []OverreservedStock `json:"overreserved"`
	FailedReceipts []FailedReceipt     `json:"failedReceipts"`
}

type OverreservedStock struct {
	VariantID     int64     `json:"variantId"`
	ProductName   string    `json:"productName"`
	VariantLabel  string    `json:"variantLabel"`
	WarehouseName string    `json:"warehouseName"`
	Available     int       `json:"available"`
	Reserved      int       `json:"reserved"`
	SyncedAt      time.Time `json:"syncedAt"`
	OrderNumbers  []string  `json:"orderNumbers"`
}

type FailedReceipt struct {
	BatchID     int64     `json:"batchId"`
	OrderID     int64     `json:"orderId"`
	OrderNumber string    `json:"orderNumber"`
	LineID      int64     `json:"lineId"`
	ProductName string    `json:"productName"`
	Error       string    `json:"error"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (probe *Probe) Diagnose(ctx context.Context) (Diagnostics, error) {
	result := Diagnostics{Overreserved: []OverreservedStock{}, FailedReceipts: []FailedReceipt{}}
	rows, err := probe.pool.Query(ctx, `
		SELECT variant.id, product.name, variant.label, warehouse.name,
		       stock.available_qty, stock.reserved_qty, stock.synced_at,
		       ARRAY(SELECT DISTINCT purchase.order_number
		             FROM order_items item JOIN orders purchase ON purchase.id = item.order_id
		             WHERE item.variant_id = variant.id AND item.reserved_qty > 0
		               AND purchase.stock_released_at IS NULL ORDER BY purchase.order_number)
		FROM inventory stock
		JOIN product_variants variant ON variant.id = stock.variant_id
		JOIN products product ON product.id = variant.product_id
		JOIN warehouses warehouse ON warehouse.id = stock.warehouse_id
		WHERE stock.reserved_qty > GREATEST(stock.available_qty, 0)
		ORDER BY stock.reserved_qty - GREATEST(stock.available_qty, 0) DESC, stock.id
		LIMIT 100`)
	if err != nil {
		return Diagnostics{}, fmt.Errorf("query overreserved stock: %w", err)
	}
	for rows.Next() {
		var item OverreservedStock
		if err := rows.Scan(&item.VariantID, &item.ProductName, &item.VariantLabel, &item.WarehouseName,
			&item.Available, &item.Reserved, &item.SyncedAt, &item.OrderNumbers); err != nil {
			rows.Close()
			return Diagnostics{}, err
		}
		result.Overreserved = append(result.Overreserved, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Diagnostics{}, err
	}
	rows.Close()

	rows, err = probe.pool.Query(ctx, `
		SELECT batch.id, purchase.id, purchase.order_number, line.id, line.raw_name,
		       failed.error_message, failed.created_at
		FROM procurement_action_items failed
		JOIN procurement_action_batches batch ON batch.id = failed.batch_id
		JOIN procurement_order_lines line ON line.id = failed.procurement_order_line_id
		JOIN procurement_orders purchase ON purchase.id = batch.procurement_order_id
		WHERE failed.status = 'failed' AND failed.channel = 'saby_receipt'
		  AND batch.status <> 'cancelled'
		  AND NOT EXISTS (
		    SELECT 1 FROM procurement_action_items resolved
		    WHERE resolved.procurement_order_line_id = failed.procurement_order_line_id
		      AND resolved.channel = failed.channel AND resolved.id > failed.id
		      AND resolved.status = 'completed')
		ORDER BY failed.created_at, failed.id LIMIT 100`)
	if err != nil {
		return Diagnostics{}, fmt.Errorf("query failed receipts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item FailedReceipt
		if err := rows.Scan(&item.BatchID, &item.OrderID, &item.OrderNumber, &item.LineID,
			&item.ProductName, &item.Error, &item.CreatedAt); err != nil {
			return Diagnostics{}, err
		}
		result.FailedReceipts = append(result.FailedReceipts, item)
	}
	if err := rows.Err(); err != nil {
		return Diagnostics{}, err
	}
	return result, nil
}
