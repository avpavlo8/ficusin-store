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
