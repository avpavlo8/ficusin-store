package order

import (
	"context"
	"errors"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

// Retrying a rejected registration leaves the packing revision and payments
// intact. The existing UUID must be removed before the normal create queue
// can claim the order under a new merchant reference.
func (worker *ShippingWorker) retryRejectedShipments(ctx context.Context) {
	rows, err := worker.pool.Query(ctx, `
		WITH candidates AS (
			SELECT id FROM orders
			WHERE cdek_create_state='retry_cancel' AND cdek_uuid<>''
				AND status NOT IN ('cancelled','completed','shipped')
				AND (cdek_next_attempt_at IS NULL OR cdek_next_attempt_at<=CURRENT_TIMESTAMP)
			ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 20
		)
		UPDATE orders o SET cdek_cancel_attempts=cdek_cancel_attempts+1,
			cdek_next_attempt_at=CURRENT_TIMESTAMP+INTERVAL '15 minutes'
		FROM candidates c WHERE o.id=c.id
		RETURNING o.id,o.cdek_uuid,o.cdek_cancel_attempts`)
	if err != nil {
		worker.logger.Error("claim CDEK registration retries", "error", err)
		return
	}
	type retry struct {
		id       int64
		uuid     string
		attempts int
	}
	var items []retry
	for rows.Next() {
		var item retry
		if err = rows.Scan(&item.id, &item.uuid, &item.attempts); err != nil {
			break
		}
		items = append(items, item)
	}
	rows.Close()
	if err == nil {
		err = rows.Err()
	}
	if err != nil {
		worker.logger.Error("read CDEK registration retries", "error", err)
		return
	}

	for _, item := range items {
		err = worker.cdek.CancelRejectedOrder(ctx, item.uuid)
		if err != nil {
			state := "retry_cancel"
			if item.attempts >= 8 {
				state = "retry_cancel_failed"
			}
			if errors.Is(err, integration.ErrCDEKRegistrationChanged) {
				state = "registered"
			}
			_, err = worker.pool.Exec(ctx, `
				UPDATE orders SET cdek_create_state=$4,cdek_last_error=$5,
					cdek_next_attempt_at=CASE WHEN $4='retry_cancel' THEN CURRENT_TIMESTAMP+INTERVAL '30 seconds' END
				WHERE id=$1 AND cdek_uuid=$2 AND cdek_cancel_attempts=$3 AND cdek_create_state='retry_cancel'`,
				item.id, item.uuid, item.attempts, state, boundedCDEKError(err.Error()))
		} else {
			_, err = worker.pool.Exec(ctx, `
				UPDATE orders
				SET cdek_create_state='retry_queued', cdek_uuid='', cdek_status='', cdek_status_reason='',
					cdek_order_number=order_number||'-s'||shipment_revision::text||'-'||cdek_attempts::text,
					cdek_next_attempt_at=CURRENT_TIMESTAMP, cdek_last_error='', cdek_synced_at=NULL,
					cdek_cancel_state='', cdek_cancel_attempts=0, cdek_cancel_next_attempt_at=NULL
				WHERE id=$1 AND cdek_uuid=$2 AND cdek_cancel_attempts=$3 AND cdek_create_state='retry_cancel'
					AND cdek_track_number='' AND status NOT IN ('cancelled','completed','shipped')`,
				item.id, item.uuid, item.attempts)
		}
		if err != nil {
			worker.logger.Error("store CDEK registration retry", "order_id", item.id, "error", err)
		}
	}
}

func boundedCDEKError(message string) string {
	runes := []rune(message)
	return string(runes[:min(len(runes), 1000)])
}
