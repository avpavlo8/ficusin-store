package procurement

import (
	"context"
	"log/slog"
	"time"
)

type ActionWorker struct {
	store    Store
	executor Executor
	logger   *slog.Logger
	interval time.Duration
}

func NewActionWorker(store Store, executor Executor, logger *slog.Logger) *ActionWorker {
	return &ActionWorker{store: store, executor: executor, logger: logger, interval: 3 * time.Second}
}

func (worker *ActionWorker) Run(ctx context.Context) {
	if worker.executor == nil {
		return
	}
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()
	for {
		worker.runOne(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (worker *ActionWorker) runOne(ctx context.Context) {
	item, err := worker.store.ClaimAction(ctx)
	if err != nil {
		worker.logger.Error("claim procurement action failed", "error", err)
		return
	}
	if item == nil {
		return
	}
	result, executeErr := worker.executor.Execute(ctx, *item)
	if err := worker.store.FinishAction(ctx, item.ID, result, executeErr); err != nil {
		worker.logger.Error("finish procurement action failed", "action_id", item.ID, "error", err)
		return
	}
	if executeErr != nil {
		worker.logger.Warn("procurement action failed", "action_id", item.ID, "channel", item.Channel, "error", executeErr)
	}
}
