package order

import (
	"context"

	"github.com/avpavlo8/ficusin-store/backend/internal/mail"
)

// Registration runs after checkout. Queue the tracking message exactly once,
// in the same transaction that marks it queued. Never recreate a shipment.
func (worker *ShippingWorker) queueTrackingLetters(ctx context.Context) {
	tx, err := worker.pool.Begin(ctx)
	if err != nil {
		worker.logger.Error("begin tracking letters", "error", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT o.id, o.order_number, COALESCE(c.full_name,''), o.email, o.cdek_track_number
		FROM orders o LEFT JOIN customers c ON c.id=o.customer_id
		WHERE o.delivery_method='cdek' AND o.status NOT IN ('cancelled','completed')
		AND o.cdek_track_number<>'' AND COALESCE(o.email,'')<>'' AND o.cdek_tracking_notified_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM order_packing_workflows p WHERE p.order_id=o.id AND p.state IN ('queued','waiting_cdek','preparing_payment','notification_queued','notified'))
		ORDER BY o.id FOR UPDATE OF o SKIP LOCKED LIMIT 20`)
	if err != nil {
		worker.logger.Error("read tracking letters", "error", err)
		return
	}
	type pending struct {
		id    int64
		email string
		order mail.OrderLetter
	}
	items := []pending{}
	for rows.Next() {
		var item pending
		if err = rows.Scan(&item.id, &item.order.Number, &item.order.CustomerName, &item.email, &item.order.TrackNumber); err != nil {
			break
		}
		items = append(items, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		worker.logger.Error("scan tracking letters", "error", err)
		return
	}
	for _, item := range items {
		letter := mail.Tracking(item.order)
		if _, err = tx.Exec(ctx, `INSERT INTO outbox(recipient,subject,body) VALUES($1,$2,$3)`, item.email, letter.Subject, letter.Body); err != nil {
			break
		}
		if _, err = tx.Exec(ctx, `UPDATE orders SET cdek_tracking_notified_at=CURRENT_TIMESTAMP WHERE id=$1`, item.id); err != nil {
			break
		}
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		worker.logger.Error("queue tracking letters", "error", err)
	}
}
