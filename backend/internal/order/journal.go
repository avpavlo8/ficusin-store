package order

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Виды движений товара по настоящему складу.
const (
	MovementReserve  = "reserve"
	MovementRelease  = "release"
	MovementWriteoff = "writeoff"
)

// RecordMovement записывает в журнал, что заказ сделал с настоящим складом.
//
// Пока запись остатков в СБИС выключена, журнал — единственное, что
// происходит: движения копятся со статусом «не отправлено», и по ним видно,
// что магазин собирался сделать со складом. Сверить это с действительностью
// дешевле, чем разбирать последствия неверного списания.
//
// Пишем в той же транзакции, что и сам заказ: движение, потерявшееся при
// откате, было бы хуже отсутствующего.
func RecordMovement(ctx context.Context, tx pgx.Tx, orderID int64, kind string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO stock_movements (order_id, variant_id, saby_id, kind, quantity, status)
		SELECT oi.order_id, oi.variant_id, COALESCE(pv.saby_id, ''), $2, SUM(oi.quantity)::INTEGER, 'pending'
		FROM order_items oi
		LEFT JOIN product_variants pv ON pv.id = oi.variant_id
		WHERE oi.order_id = $1 AND oi.variant_id IS NOT NULL AND oi.is_preorder = 0
		GROUP BY oi.order_id, oi.variant_id, pv.saby_id
	`, orderID, kind); err != nil {
		return fmt.Errorf("record stock movement %s: %w", kind, err)
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
		-- order_items.product_id is the legacy public slug (TEXT), while
		-- products.id is BIGINT. The variant is the real relational key and
		-- is present on every order created by the current checkout.
		JOIN product_variants pv ON pv.id = oi.variant_id
		LEFT JOIN products p ON p.id = pv.product_id
		LEFT JOIN saby_nomenclature n ON n.saby_id = p.saby_id
		WHERE oi.order_id = $1 AND oi.is_preorder = 1
		GROUP BY n.saby_id, oi.product_name, o.id, o.order_number
`

func recordPreorderRequests(ctx context.Context, tx pgx.Tx, orderID int64) error {
	if _, err := tx.Exec(ctx, recordPreorderRequestsSQL, orderID); err != nil {
		return fmt.Errorf("record preorder requests: %w", err)
	}
	return nil
}
