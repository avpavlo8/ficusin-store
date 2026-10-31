package order

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/notify"
	"github.com/avpavlo8/ficusin-store/backend/internal/webpush"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Order notifications use the explicit order-push opt-in from the profile.
// Chat subscriptions have a separate opt-in and are deliberately not reused.
func queueStaffOrderPush(ctx context.Context, tx pgx.Tx, orderID int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO staff_order_push_outbox(order_id,subscription_id,customer_id)
 SELECT $1,s.id,s.customer_id FROM push_subscriptions s
 JOIN admin_users a ON a.customer_id=s.customer_id
 WHERE s.expired_at IS NULL AND a.is_active AND a.role IN ('manager','administrator','owner')
 ON CONFLICT(order_id,subscription_id) DO NOTHING`, orderID)
	return err
}

type StaffOrderPushWorker struct {
	pool   *pgxpool.Pool
	sender packingPushSender
	logger *slog.Logger
}

func NewStaffOrderPushWorker(pool *pgxpool.Pool, sender packingPushSender, logger *slog.Logger) *StaffOrderPushWorker {
	return &StaffOrderPushWorker{pool: pool, sender: sender, logger: logger}
}
func (w *StaffOrderPushWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		w.process(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (w *StaffOrderPushWorker) process(ctx context.Context) {
	for range 20 {
		ok, err := w.deliver(ctx)
		if err != nil {
			w.logger.Error("staff order push delivery failed", "error", err)
			return
		}
		if !ok {
			return
		}
	}
}
func (w *StaffOrderPushWorker) deliver(ctx context.Context) (bool, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id, subscription, orderID int64
	var number string
	var eligible, old bool
	var sub notify.Subscription
	err = tx.QueryRow(ctx, `SELECT d.id,s.id,o.id,o.order_number,s.endpoint,s.p256dh,s.auth,
 COALESCE(s.customer_id=d.customer_id AND s.expired_at IS NULL AND a.is_active AND a.role IN ('manager','administrator','owner'),false),
 d.created_at<CURRENT_TIMESTAMP-INTERVAL '24 hours'
 FROM staff_order_push_outbox d JOIN orders o ON o.id=d.order_id
 JOIN push_subscriptions s ON s.id=d.subscription_id
 LEFT JOIN admin_users a ON a.customer_id=d.customer_id
 WHERE d.completed_at IS NULL AND d.next_attempt_at<=CURRENT_TIMESTAMP
 ORDER BY d.id LIMIT 1 FOR UPDATE OF d SKIP LOCKED`).Scan(&id, &subscription, &orderID, &number, &sub.Endpoint, &sub.P256dh, &sub.Auth, &eligible, &old)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	outcome := "sent"
	switch {
	case !eligible:
		outcome = "inactive"
	case old:
		outcome = "expired"
	default:
		if w.sender == nil || w.sender.PublicKey() == "" {
			return false, errors.New("staff order push is not configured")
		}
		err = w.sender.Deliver(ctx, sub, notify.Message{Title: "Новый заказ " + number, Body: "На сайте оформлен заказ. Откройте его в панели управления.", URL: fmt.Sprintf("/admin?section=orders&order=%d", orderID), Tag: fmt.Sprintf("staff-order-%d", orderID)})
		switch {
		case errors.Is(err, webpush.ErrGone):
			if _, err = tx.Exec(ctx, `UPDATE push_subscriptions SET expired_at=CURRENT_TIMESTAMP WHERE id=$1`, subscription); err != nil {
				return false, err
			}
			outcome = "gone"
		case err != nil:
			_, err = tx.Exec(ctx, `UPDATE staff_order_push_outbox SET attempts=attempts+1,last_error=$2,next_attempt_at=CURRENT_TIMESTAMP+make_interval(secs=>LEAST(3600,5*(1<<LEAST(attempts,9)))) WHERE id=$1`, id, err.Error())
			if err != nil {
				return false, err
			}
			return true, tx.Commit(ctx)
		default:
			if _, err = tx.Exec(ctx, `UPDATE push_subscriptions SET last_sent_at=CURRENT_TIMESTAMP WHERE id=$1`, subscription); err != nil {
				return false, err
			}
		}
	}
	_, err = tx.Exec(ctx, `UPDATE staff_order_push_outbox SET completed_at=CURRENT_TIMESTAMP,outcome=$2,last_error='' WHERE id=$1`, id, outcome)
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
