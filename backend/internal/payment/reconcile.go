package payment

import (
	"context"
	"log/slog"
	"time"
)

// reconcileInterval is how often we ask YooKassa about payments we are still
// waiting on. A minute is fast enough that nobody notices, and cheap: only
// unfinished payments are asked about.
const reconcileInterval = time.Minute

// reconcileWindow is how long a pending payment stays interesting. People
// pay within minutes of being sent to the payment page; a payment nobody
// finished in two hours was abandoned there. Asking about it all day would
// be thousands of pointless requests, and if such a payment is somehow
// completed later, the notification still brings it in.
const reconcileWindow = 2 * time.Hour

// ReconcileWorker is the safety net under the notifications.
//
// YooKassa notifies us when a payment succeeds, but a notification can be
// lost, arrive while we are restarting, or never be configured at all — the
// last of which is exactly what happened on the first live test. Without
// this, the shop would hold the money and still show the customer an unpaid
// order with a "Pay" button on it.
type ReconcileWorker struct {
	service  *Service
	logger   *slog.Logger
	interval time.Duration
}

func NewReconcileWorker(service *Service, logger *slog.Logger) *ReconcileWorker {
	return &ReconcileWorker{service: service, logger: logger, interval: reconcileInterval}
}

func (worker *ReconcileWorker) Run(ctx context.Context) {
	if worker.service == nil || !worker.service.Configured() {
		return
	}
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.process(ctx)
		}
	}
}

func (worker *ReconcileWorker) process(ctx context.Context) {
	// Only payments that could still matter: a cancelled order has nothing
	// left to pay for, and asking about it would be traffic for nothing.
	rows, err := worker.service.pool.Query(ctx, `
		SELECT p.provider_payment_id
		FROM payments p
		JOIN orders o ON o.id = p.order_id
		WHERE p.status = $1
			AND p.provider_payment_id <> ''
			AND p.created_at > CURRENT_TIMESTAMP - $2::INTERVAL
			AND o.status <> 'cancelled'
			AND o.payment_status <> $3
		ORDER BY p.id
		LIMIT 50
	`, StatusPending, reconcileWindow.String(), StatusPaid)
	if err != nil {
		worker.logger.Error("reconcile query failed", "error", err)
		return
	}
	pending := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			worker.logger.Error("reconcile scan failed", "error", err)
			break
		}
		pending = append(pending, id)
	}
	rows.Close()

	for _, id := range pending {
		// One payment failing to check says nothing about the others, so a
		// failure here is logged and the loop carries on.
		if err := worker.service.Sync(ctx, id); err != nil {
			worker.logger.Error("reconcile payment failed", "error", err, "payment_id", id)
		}
	}
}
