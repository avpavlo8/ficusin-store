package order

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const recordStockReleaseSQL = `
	INSERT INTO stock_movements(order_id, variant_id, saby_id, kind, quantity, status)
	SELECT oi.order_id, oi.variant_id, COALESCE(pv.saby_id, ''), 'release',
		SUM(oi.reserved_qty)::INTEGER, 'pending'
	FROM order_items oi
	LEFT JOIN product_variants pv ON pv.id = oi.variant_id
	WHERE oi.order_id = $1 AND oi.variant_id IS NOT NULL AND oi.reserved_qty > 0
	GROUP BY oi.order_id, oi.variant_id, pv.saby_id
`

// RecordStockRelease preserves the journal of reservations created before
// checkout stopped reserving stock. New orders have no reservation to release.
// Callers release the inventory in the same transaction; this audit entry never
// writes off physical Saby stock or schedules a provider request.
func RecordStockRelease(ctx context.Context, tx pgx.Tx, orderID int64) error {
	if _, err := tx.Exec(ctx, recordStockReleaseSQL, orderID); err != nil {
		return fmt.Errorf("record stock release: %w", err)
	}
	return nil
}

// recordPreorderRequests переносит предзаказные строки заказа в очередь
// закупки.
//
// Товар связывается с номенклатурой СБИС, потому что рекомендации считают
// спрос именно по ней. Строка без такой связи всё равно сохраняется: пусть
// менеджер увидит «клиент ждёт», даже если карточка ещё не сопоставлена, —
// молча потерять обещание покупателю хуже, чем показать несопоставленную
// заявку.
const recordPreorderRequestsSQL = `
		INSERT INTO procurement_requests (
			kind, saby_id, requested_name, quantity, customer_order_id, status, notes
		)
		SELECT 'customer_order', n.saby_id, oi.product_name,
			SUM(oi.quantity)::INTEGER, o.id, 'open',
			'Предзаказ клиента, заказ ' || o.order_number
		FROM order_items oi
		JOIN orders o ON o.id = oi.order_id
		JOIN product_variants pv ON pv.id = oi.variant_id
		LEFT JOIN saby_nomenclature n ON n.saby_id = pv.saby_id
		WHERE oi.order_id = $1 AND oi.is_preorder = 1
		GROUP BY n.saby_id, oi.product_name, o.id, o.order_number
`

func recordPreorderRequests(ctx context.Context, tx pgx.Tx, orderID int64) error {
	if _, err := tx.Exec(ctx, recordPreorderRequestsSQL, orderID); err != nil {
		return fmt.Errorf("record preorder requests: %w", err)
	}
	return nil
}
