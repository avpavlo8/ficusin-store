package procurement

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type ActionWorker struct {
	store    ActionWorkerStore
	executor GroupExecutor
	logger   *slog.Logger
	interval time.Duration
	owner    string
}

// ActionWorkerStore requires the durable group claim and last-boundary price
// guard used by the production store. Individual Saby/Avito actions are still
// selected by ClaimActionGroup through its single-action delegate.
type ActionWorkerStore interface {
	ActionGroupStore
	GuardClaimedPriceActions(context.Context, []ActionItem) (bool, error)
	FinishAction(context.Context, int64, string, int64, ActionExecution, error) (bool, error)
}

func (worker *ActionWorker) priceActionsCurrent(ctx context.Context, items []ActionItem) bool {
	current, err := worker.store.GuardClaimedPriceActions(ctx, items)
	if err != nil {
		worker.logger.Error("check site price before procurement export failed", "error", err)
		return false
	}
	if !current {
		worker.logger.Warn("skipped stale procurement price export", "action_id", items[0].ID)
	}
	return current
}

func NewActionWorker(store ActionWorkerStore, executor GroupExecutor, logger *slog.Logger) *ActionWorker {
	return &ActionWorker{store: store, executor: executor, logger: logger, interval: 3 * time.Second, owner: fmt.Sprintf("actions-%d", time.Now().UnixNano())}
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

	items, err := worker.store.ClaimActionGroup(ctx, worker.owner)
	if err != nil {
		worker.logger.Error("claim procurement action group failed", "error", err)
		return
	}
	if len(items) == 0 {
		return
	}
	if !worker.priceActionsCurrent(ctx, items) {
		return
	}
	for _, outcome := range worker.executor.ExecuteGroup(ctx, items) {
		item := items[0]
		for _, candidate := range items {
			if candidate.ID == outcome.ItemID {
				item = candidate
				break
			}
		}
		if applied, err := worker.store.FinishAction(ctx, outcome.ItemID, item.LockOwner, item.LockToken, outcome.Result, outcome.Err); err != nil {
			worker.logger.Error("finish procurement action failed", "action_id", outcome.ItemID, "error", err)
			continue
		} else if !applied {
			worker.logger.Warn("ignored stale procurement action finisher", "action_id", outcome.ItemID, "lease_token", item.LockToken)
			continue
		}
		if outcome.Err != nil {
			worker.logger.Warn("procurement action failed", "action_id", outcome.ItemID, "error", outcome.Err)
		}
	}
}
