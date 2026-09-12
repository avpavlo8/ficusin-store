package procurement

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type CatalogSyncStore interface {
	IntegrationSyncCoordinator
	RememberChannelProducts(context.Context, string, []ChannelProduct) error
	LinkChannelProducts(context.Context, Actor, string, []ChannelProduct) (ChannelLinkResult, error)
}

type CatalogSyncSource interface {
	Executor
	ChannelCatalogSource
	SabyCatalogRefresher
}

// CatalogWorker is the only owner of Ozon and Saby catalogue reads. WB keeps
// its existing dedicated mirror worker. UI requests only advance a generation.
type CatalogWorker struct {
	store  CatalogSyncStore
	source CatalogSyncSource
	logger *slog.Logger
	owner  string
	poll   time.Duration
}

func NewCatalogWorker(store CatalogSyncStore, source CatalogSyncSource, logger *slog.Logger) *CatalogWorker {
	return &CatalogWorker{store: store, source: source, logger: logger, owner: fmt.Sprintf("catalog-%d", time.Now().UnixNano()), poll: time.Minute}
}

func (worker *CatalogWorker) Run(ctx context.Context) {
	if worker.store == nil || worker.source == nil {
		return
	}
	worker.run(ctx)
	ticker := time.NewTicker(worker.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.run(ctx)
		}
	}
}

func (worker *CatalogWorker) run(ctx context.Context) {
	for _, channel := range []string{"saby", "ozon"} {
		if !worker.source.Configured(channel) {
			continue
		}
		claim, err := worker.store.ClaimIntegrationSync(ctx, channel, "catalog", worker.owner, 20*time.Minute)
		if err != nil {
			worker.logger.Error("claim catalogue synchronization failed", "channel", channel, "error", err)
			continue
		}
		if claim == nil {
			continue
		}
		rows := 0
		var syncErr error
		if channel == "saby" {
			var result ChannelLinkResult
			result, syncErr = worker.source.RefreshSabyCatalog(ctx)
			rows = result.Fetched
		} else {
			var items []ChannelProduct
			items, syncErr = worker.source.FetchCatalog(ctx, channel)
			rows = len(items)
			if syncErr == nil {
				syncErr = worker.store.RememberChannelProducts(ctx, channel, items)
			}
			if syncErr == nil {
				_, syncErr = worker.store.LinkChannelProducts(ctx, Actor{Role: "system"}, channel, items)
			}
		}
		now := time.Now().UTC()
		retry := retryDelay(syncErr)
		applied, finishErr := worker.store.FinishIntegrationSync(ctx, *claim, rows, now, now, nil, retry, syncErr)
		if finishErr != nil {
			worker.logger.Error("finish catalogue synchronization failed", "channel", channel, "error", finishErr)
			continue
		}
		if !applied {
			worker.logger.Warn("ignored stale catalogue finisher", "channel", channel, "lease_token", claim.Token)
		}
	}
}
