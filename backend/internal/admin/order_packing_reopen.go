package admin

import (
	"context"
	"errors"
	"fmt"
)

// RequestOrderPackingReopen retires payment attempts and notifications atomically.
// The carrier worker unlocks composition only after confirming removal of the old UUID.
func (repository *PostgresRepository) RequestOrderPackingReopen(ctx context.Context, actor Actor, id int64, revision int) (OrderPackingState, error) {
	if !Can(actor.Role, PermissionOrdersEdit) {
		return OrderPackingState{}, ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return OrderPackingState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7241,$1::integer)`, id); err != nil {
		return OrderPackingState{}, err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM payments WHERE order_id=$1 AND status IN ('pending','unknown') ORDER BY id`, id)
	if err != nil {
		return OrderPackingState{}, err
	}
	ids := []int64{}
	for rows.Next() {
		var p int64
		if err = rows.Scan(&p); err != nil {
			rows.Close()
			return OrderPackingState{}, err
		}
		ids = append(ids, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return OrderPackingState{}, err
	}
	for _, p := range ids {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, -p); err != nil {
			return OrderPackingState{}, err
		}
	}
	var current int
	var status, uuid, carrier, carrierStatus string
	if err = tx.QueryRow(ctx, `SELECT shipment_revision,status,cdek_uuid,cdek_create_state,cdek_status FROM orders WHERE id=$1 FOR UPDATE`, id).Scan(&current, &status, &uuid, &carrier, &carrierStatus); err != nil {
		return OrderPackingState{}, err
	}
	if current != revision {
		return OrderPackingState{}, errors.New("заказ изменился — обновите карточку")
	}
	if status == "shipped" || status == "completed" || status == "cancelled" {
		return OrderPackingState{}, errors.New("заказ уже передан перевозчику или закрыт; возврат на редактирование недоступен")
	}
	var workflow int64
	var state string
	if err = tx.QueryRow(ctx, `SELECT id,state FROM order_packing_workflows WHERE order_id=$1 AND state NOT IN ('cancelled','stale') FOR UPDATE`, id).Scan(&workflow, &state); err != nil {
		return OrderPackingState{}, err
	}
	if state == "reopening" {
		_ = tx.Rollback(ctx)
		return repository.OrderPacking(ctx, id)
	}
	if carrier == "creating" || carrier == "unknown" {
		return OrderPackingState{}, errors.New("СДЭК ещё подтверждает создание заявки; дождитесь результата перед редактированием")
	}
	if uuid != "" && carrierStatus != "CREATED" && carrierStatus != "INVALID" && carrierStatus != "REMOVED" {
		return OrderPackingState{}, errors.New("статус СДЭК требует сверки перед отменой заявки")
	}
	var unsafe bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payments WHERE order_id=$1 AND (status='unknown' OR (status='pending' AND provider_payment_id='' AND request_payload IS NOT NULL) OR (status='pending' AND (provider_payment_id<>'' OR request_payload IS NOT NULL) AND COALESCE(request_payload->'Metadata'->>'ficusin_capture_mode',request_payload->'metadata'->>'ficusin_capture_mode','')<>'two_stage')))`, id).Scan(&unsafe); err != nil {
		return OrderPackingState{}, err
	}
	if unsafe {
		return OrderPackingState{}, errors.New("есть платёж с неизвестным результатом или ссылка старого типа; сначала сверьте оплату в ЮKassa")
	}
	if _, err = tx.Exec(ctx, `UPDATE payments SET status='superseded',updated_at=CURRENT_TIMESTAMP WHERE order_id=$1 AND status='pending'`, id); err != nil {
		return OrderPackingState{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE order_packing_workflows SET state='reopening',notified_at=NULL,payment_url='',last_error='',locked_until=NULL,next_attempt_at=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, workflow); err != nil {
		return OrderPackingState{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE outbox SET cancelled_at=CURRENT_TIMESTAMP,cancel_reason='packing_reopened' WHERE order_packing_id=$1 AND sent_at IS NULL AND cancelled_at IS NULL`, workflow); err != nil {
		return OrderPackingState{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE order_packing_push_outbox SET cancelled_at=CURRENT_TIMESTAMP WHERE id IN (SELECT id FROM order_packing_push_outbox WHERE workflow_id=$1 AND sent_at IS NULL AND cancelled_at IS NULL FOR UPDATE SKIP LOCKED)`, workflow); err != nil {
		return OrderPackingState{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE orders SET delivery_fee_pending=1,cdek_checkout_requested=0 WHERE id=$1`, id); err != nil {
		return OrderPackingState{}, err
	}
	if err = insertAudit(ctx, tx, actor, "order.packing.reopen", "order", fmt.Sprint(id), map[string]any{"revision": current, "cdekUUID": uuid}, map[string]any{"state": "reopening"}); err != nil {
		return OrderPackingState{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return OrderPackingState{}, err
	}
	if repository.packingWake != nil {
		repository.packingWake()
	}
	return repository.OrderPacking(ctx, id)
}
