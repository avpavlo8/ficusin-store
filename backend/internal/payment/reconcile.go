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

// reconcileWindow is how long a pending payment stays interesting. A payment
// nobody finished within a day was abandoned on the payment page, and asking
// about it forever would be pointless traffic.
const reconcileWindow = 24 * time.Hour

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
	rows, err := worker.service.pool.Query(ctx, `
		SELECT provider_payment_id
		FROM payments
		WHERE status = $1
			AND provider_payment_id <> ''
			AND created_at > CURRENT_TIMESTAMP - $2::INTERVAL
		ORDER BY id
		LIMIT 50
	`, StatusPending, reconcileWindow.String())
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
