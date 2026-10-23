package order

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

func (worker *ShippingWorker) reopenPackings(ctx context.Context) {
	rows, err := worker.pool.Query(ctx, `WITH picked AS (SELECT id FROM order_packing_workflows WHERE state='reopening' AND (locked_until IS NULL OR locked_until<CURRENT_TIMESTAMP) AND (next_attempt_at IS NULL OR next_attempt_at<=CURRENT_TIMESTAMP) ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 20) UPDATE order_packing_workflows w SET locked_until=CURRENT_TIMESTAMP+INTERVAL '5 minutes' FROM picked WHERE w.id=picked.id RETURNING w.id`)
	if err != nil {
		worker.logger.Error("claim packing reopen failed", "error", err)
		return
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err = worker.reopenPacking(ctx, id); err != nil {
			worker.logger.Error("reopen packing failed", "error", err, "packing_id", id)
			_, _ = worker.pool.Exec(ctx, `UPDATE order_packing_workflows SET last_error=$2,locked_until=NULL,next_attempt_at=CURRENT_TIMESTAMP+INTERVAL '1 minute',updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND state='reopening'`, id, err.Error())
		}
	}
}

func (worker *ShippingWorker) reopenPacking(ctx context.Context, workflowID int64) error {
	var orderID int64
	if err := worker.pool.QueryRow(ctx, `SELECT order_id FROM order_packing_workflows WHERE id=$1`, workflowID).Scan(&orderID); err != nil {
		return err
	}
	tx, err := worker.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7241,$1::integer)`, orderID); err != nil {
		return err
	}
	var uuid, status, state string
	var revision, savedRevision int
	if err = tx.QueryRow(ctx, `SELECT cdek_uuid,status,shipment_revision FROM orders WHERE id=$1 FOR UPDATE`, orderID).Scan(&uuid, &status, &revision); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT state,order_revision FROM order_packing_workflows WHERE id=$1 FOR UPDATE`, workflowID).Scan(&state, &savedRevision); err != nil {
		return err
	}
	if state != "reopening" {
		return nil
	}
	if status == "shipped" || status == "completed" || status == "cancelled" || revision != savedRevision {
		return errors.New("заказ закрыт, передан перевозчику или изменился; нужна проверка менеджера")
	}
	if uuid != "" {
		shipment, lookupErr := worker.cdek.FetchOrder(ctx, uuid)
		if lookupErr != nil && !errors.Is(lookupErr, integration.ErrCDEKOrderNotFound) {
			return lookupErr
		}
		if lookupErr == nil {
			switch strings.ToUpper(shipment.Status) {
			case "CREATED", "INVALID", "REMOVED":
			default:
				return fmt.Errorf("СДЭК: статус %s не допускает возврат на редактирование; проверьте отправление", shipment.Status)
			}
			if shipment.Status != "REMOVED" {
				if err = worker.cdek.CancelOrder(ctx, uuid); err != nil {
					return err
				}
			}
		}
	}
	// A confirmed deletion is the only point where the old UUID can be cleared.
	// Keep the previous boxes and carrier identity in workflow/audit history.
	if _, err = tx.Exec(ctx, `UPDATE order_packing_workflows SET state='stale',last_error='',locked_until=NULL,next_attempt_at=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, workflowID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE orders SET shipment_revision=shipment_revision+1,cdek_order_number=order_number||'-r'||(shipment_revision+1)::text,cdek_uuid='',cdek_track_number='',cdek_status='',cdek_status_reason='',cdek_create_state='new',cdek_cancel_state='',cdek_cancel_attempts=0,cdek_cancel_next_attempt_at=NULL,cdek_attempts=0,cdek_next_attempt_at=NULL,cdek_last_error='',cdek_tracking_notified_at=NULL,cdek_checkout_requested=0,delivery_fee_pending=1,delivery_repack_requested=1,cdek_tariff_code=NULL,status='new' WHERE id=$1`, orderID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
