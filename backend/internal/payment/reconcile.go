package payment

import (
	"context"
	"log/slog"
	"time"
)

const reconcileInterval = time.Minute
const reconcileWindow = 2 * time.Hour

type ReconcileWorker struct {
	service  *Service
	logger   *slog.Logger
	interval time.Duration
}

func NewReconcileWorker(service *Service, logger *slog.Logger) *ReconcileWorker {
	return &ReconcileWorker{service: service, logger: logger, interval: reconcileInterval}
}

func (worker *ReconcileWorker) Run(ctx context.Context) {
	if worker.service == nil || !worker.service.Configured() { return }
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done(): return
		case <-ticker.C: worker.process(ctx)
		}
	}
}

func (worker *ReconcileWorker) process(ctx context.Context) {
	rows, err := worker.service.pool.Query(ctx, `
		SELECT p.provider_payment_id
		FROM payments p JOIN orders o ON o.id=p.order_id
		WHERE p.status=$1 AND p.provider_payment_id<>''
			AND p.created_at>CURRENT_TIMESTAMP-$2::INTERVAL
			AND o.status<>'cancelled'
		ORDER BY p.id LIMIT 50
	`, StatusPending, reconcileWindow.String())
	if err != nil { worker.logger.Error("reconcile query failed", "error", err); return }
	pending:=[]string{}
	for rows.Next(){var id string;if err:=rows.Scan(&id);err!=nil{worker.logger.Error("reconcile scan failed","error",err);break};pending=append(pending,id)}
	rows.Close()
	for _,id:=range pending{
		if err:=worker.service.SyncOutstanding(ctx,id);err!=nil{worker.logger.Error("reconcile payment failed","error",err,"payment_id",id)}
	}
}
