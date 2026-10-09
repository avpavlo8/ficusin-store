package procurement

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type ActionWorker struct {
	store    Store
	executor Executor
	logger   *slog.Logger
	interval time.Duration
	owner    string
}

type claimedPriceActionGuard interface {
	GuardClaimedPriceActions(context.Context, []ActionItem) (bool, error)
}

func (worker *ActionWorker) priceActionsCurrent(ctx context.Context, items []ActionItem) bool {
	guard, ok := worker.store.(claimedPriceActionGuard)
	if !ok {
		return true
	}
	current, err := guard.GuardClaimedPriceActions(ctx, items)
	if err != nil {
		worker.logger.Error("check site price before procurement export failed", "error", err)
		return false
	}
	if !current {
		worker.logger.Warn("skipped stale procurement price export", "action_id", items[0].ID)
	}
	return current
}

func NewActionWorker(store Store, executor Executor, logger *slog.Logger) *ActionWorker {
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
	groupStore, groups := worker.store.(ActionGroupStore)
	groupExecutor, executesGroups := worker.executor.(GroupExecutor)
	if groups && executesGroups {
		items, err := groupStore.ClaimActionGroup(ctx, worker.owner)
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
		for _, outcome := range groupExecutor.ExecuteGroup(ctx, items) {
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
		return
	}
	item, err := worker.store.ClaimAction(ctx, worker.owner)
	if err != nil {
		worker.logger.Error("claim procurement action failed", "error", err)
		return
	}
	if item == nil {
		return
	}
	if !worker.priceActionsCurrent(ctx, []ActionItem{*item}) {
		return
	}
	result, executeErr := worker.executor.Execute(ctx, *item)
	if applied, err := worker.store.FinishAction(ctx, item.ID, item.LockOwner, item.LockToken, result, executeErr); err != nil {
		worker.logger.Error("finish procurement action failed", "action_id", item.ID, "error", err)
		return
	} else if !applied {
		worker.logger.Warn("ignored stale procurement action finisher", "action_id", item.ID, "lease_token", item.LockToken)
		return
	}
	if executeErr != nil {
		worker.logger.Warn("procurement action failed", "action_id", item.ID, "channel", item.Channel, "error", executeErr)
	}
}
