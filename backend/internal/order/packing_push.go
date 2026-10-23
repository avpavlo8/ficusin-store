package order

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/notify"
	"github.com/avpavlo8/ficusin-store/backend/internal/webpush"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type packingPushSender interface {
	PublicKey() string
	Deliver(context.Context, notify.Subscription, notify.Message) error
}

// PackingPushWorker delivers independently of SMTP. SKIP LOCKED serializes each
// browser delivery; a stable tag replaces a duplicate after an ambiguous result.
type PackingPushWorker struct {
	pool   *pgxpool.Pool
	sender packingPushSender
	logger *slog.Logger
}

func NewPackingPushWorker(pool *pgxpool.Pool, sender packingPushSender, logger *slog.Logger) *PackingPushWorker {
	return &PackingPushWorker{pool: pool, sender: sender, logger: logger}
}
func (w *PackingPushWorker) Run(ctx context.Context) {
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
func (w *PackingPushWorker) process(ctx context.Context) {
	for range 20 {
		ok, err := w.deliver(ctx)
		if err != nil {
			w.logger.Error("packing push delivery failed", "error", err)
			return
		}
		if !ok {
			return
		}
	}
}
func (w *PackingPushWorker) deliver(ctx context.Context) (bool, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id, workflow, subscription, orderID int64
	var sub notify.Subscription
	var number, state, status, paymentURL string
	var eligible bool
	err = tx.QueryRow(ctx, `SELECT p.id,p.workflow_id,o.id,COALESCE(s.id,0),COALESCE(s.endpoint,''),COALESCE(s.p256dh,''),COALESCE(s.auth,''),o.order_number,w.state,o.status,w.payment_url,
 COALESCE(s.customer_id=o.customer_id AND s.expired_at IS NULL AND w.order_revision=o.shipment_revision,false)
 FROM order_packing_push_outbox p JOIN order_packing_workflows w ON w.id=p.workflow_id JOIN orders o ON o.id=w.order_id LEFT JOIN push_subscriptions s ON s.id=p.subscription_id
 WHERE p.sent_at IS NULL AND p.cancelled_at IS NULL AND p.attempts<5 AND p.next_attempt_at<=CURRENT_TIMESTAMP ORDER BY p.id LIMIT 1 FOR UPDATE OF p SKIP LOCKED`).Scan(&id, &workflow, &orderID, &subscription, &sub.Endpoint, &sub.P256dh, &sub.Auth, &number, &state, &status, &paymentURL, &eligible)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Serialize cancellation/revision changes with the last eligibility check.
	// The lock is held only for this bounded push-provider request (15 seconds).
	if err = tx.QueryRow(ctx, `SELECT o.status,COALESCE(w.state IN ('notification_queued','notified') AND w.order_revision=o.shipment_revision AND s.customer_id=o.customer_id AND s.expired_at IS NULL,false) FROM orders o JOIN order_packing_workflows w ON w.id=$2 LEFT JOIN push_subscriptions s ON s.id=$3 WHERE o.id=$1 FOR UPDATE OF o`, orderID, workflow, subscription).Scan(&status, &eligible); err != nil {
		return false, err
	}
	if !eligible || status == "cancelled" || state == "cancelled" || state == "stale" {
		_, err = tx.Exec(ctx, `UPDATE order_packing_push_outbox SET cancelled_at=CURRENT_TIMESTAMP,last_error='Подписка недоступна или заказ изменился' WHERE id=$1`, id)
	} else {
		body := "Заказ подтверждён. Посмотреть доставку и оплату можно в заказе."
		if paymentURL != "" {
			body = "Заказ подтверждён. Ссылка на оплату готова."
		}
		if w.sender == nil || w.sender.PublicKey() == "" {
			err = errors.New("Push выключен: не настроены ключи VAPID")
		} else {
			err = w.sender.Deliver(ctx, sub, notify.Message{Title: "Заказ " + number, Body: body, URL: "/account/orders/" + url.PathEscape(number), Tag: fmt.Sprintf("order-packing-%d", workflow)})
		}
		switch {
		case errors.Is(err, webpush.ErrGone):
			if _, err = tx.Exec(ctx, `UPDATE push_subscriptions SET expired_at=CURRENT_TIMESTAMP WHERE id=$1`, subscription); err != nil {
				return false, err
			}
			_, err = tx.Exec(ctx, `UPDATE order_packing_push_outbox SET cancelled_at=CURRENT_TIMESTAMP,last_error='Подписка покупателя истекла' WHERE id=$1`, id)
		case err != nil:
			_, err = tx.Exec(ctx, `UPDATE order_packing_push_outbox SET attempts=attempts+1,last_error=$2,next_attempt_at=CURRENT_TIMESTAMP+make_interval(secs=>LEAST(3600,30*(1<<LEAST(attempts,7)))) WHERE id=$1`, id, err.Error())
		default:
			if _, err = tx.Exec(ctx, `UPDATE order_packing_push_outbox SET sent_at=CURRENT_TIMESTAMP,last_error='' WHERE id=$1`, id); err != nil {
				return false, err
			}
			_, err = tx.Exec(ctx, `UPDATE order_packing_workflows SET notified_at=COALESCE(notified_at,CURRENT_TIMESTAMP),updated_at=CURRENT_TIMESTAMP WHERE id=$1`, workflow)
		}
	}
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
