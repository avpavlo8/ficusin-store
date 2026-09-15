package procurement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	wbMirrorEvery = time.Hour
	wbMirrorLease = 20 * time.Minute
	wbMirrorPoll  = time.Minute
	wbSalesDays   = 90
)

// WBMirrorStore is deliberately narrower than Store: it describes the local
// Wildberries mirror and keeps the remote API out of request handlers.
type WBMirrorStore interface {
	ClaimWBSync(context.Context, string, string, time.Duration) (*SyncClaim, error)
	FinishWBSync(context.Context, SyncClaim, int, time.Duration, error) (bool, error)
	RememberChannelProducts(context.Context, string, []ChannelProduct) error
	ReplaceSales(context.Context, string, time.Time, time.Time, []SalesRecord) (int, error)
	MarkSalesSync(context.Context, string, string, error) error
}

type WBMirrorSource interface {
	Configured(string) bool
	FetchCatalog(context.Context, string) ([]ChannelProduct, error)
	FetchSales(context.Context, string, time.Time, time.Time) ([]SalesRecord, error)
}

// WBMirrorWorker is the sole owner of read-only WB exports. Catalogue, prices
// and operational sales are copied into PostgreSQL once an hour; every screen
// and procurement calculation consumes that local state.
type WBMirrorWorker struct {
	store  WBMirrorStore
	source WBMirrorSource
	logger *slog.Logger
	now    func() time.Time
	poll   time.Duration
	owner  string
}

func NewWBMirrorWorker(store WBMirrorStore, source WBMirrorSource, logger *slog.Logger) *WBMirrorWorker {
	return &WBMirrorWorker{store: store, source: source, logger: logger, now: time.Now, poll: wbMirrorPoll,
		owner: fmt.Sprintf("wb-%d", time.Now().UnixNano())}
}

func (worker *WBMirrorWorker) Run(ctx context.Context) {
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

func (worker *WBMirrorWorker) run(ctx context.Context) {
	if !worker.source.Configured("wb") {
		_ = worker.store.MarkSalesSync(ctx, "wb", "disabled", nil)
		return
	}
	worker.syncCatalog(ctx)
	worker.syncSales(ctx)
}

func (worker *WBMirrorWorker) syncCatalog(ctx context.Context) {
	claim, err := worker.store.ClaimWBSync(ctx, "catalog", worker.owner, wbMirrorLease)
	if err != nil {
		worker.logger.Error("claim Wildberries catalogue mirror failed", "error", err)
		return
	}
	if claim == nil {
		return
	}
	items, syncErr := worker.source.FetchCatalog(ctx, "wb")
	if syncErr == nil {
		syncErr = worker.store.RememberChannelProducts(ctx, "wb", items)
	}
	worker.finish(ctx, *claim, len(items), syncErr)
}

func (worker *WBMirrorWorker) syncSales(ctx context.Context) {
	claim, err := worker.store.ClaimWBSync(ctx, "sales", worker.owner, wbMirrorLease)
	if err != nil {
		worker.logger.Error("claim Wildberries sales mirror failed", "error", err)
		return
	}
	if claim == nil {
		return
	}
	to := day(worker.now().UTC())
	from := to.AddDate(0, 0, -(wbSalesDays - 1))
	_ = worker.store.MarkSalesSync(ctx, "wb", "running", nil)
	records, syncErr := worker.source.FetchSales(ctx, "wb", from, to)
	rows := 0
	if syncErr == nil {
		rows, syncErr = worker.store.ReplaceSales(ctx, "wb", from, to, records)
	}
	if syncErr != nil {
		_ = worker.store.MarkSalesSync(ctx, "wb", "error", syncErr)
	}
	worker.finish(ctx, *claim, rows, syncErr)
}

func (worker *WBMirrorWorker) finish(ctx context.Context, claim SyncClaim, rows int, syncErr error) {
	next := wbMirrorEvery
	if syncErr != nil {
		next = 15 * time.Minute
		var retryable interface{ RetryDelay() time.Duration }
		if errors.As(syncErr, &retryable) && retryable.RetryDelay() > 0 {
			next = retryable.RetryDelay()
		}
		worker.logger.Warn("Wildberries mirror failed", "resource", claim.Resource, "retry_after", next, "error", syncErr)
	}
	applied, err := worker.store.FinishWBSync(ctx, claim, rows, next, syncErr)
	if err != nil {
		worker.logger.Error("finish Wildberries mirror failed", "resource", claim.Resource, "error", err)
	} else if !applied {
		worker.logger.Warn("ignored stale Wildberries mirror finisher", "resource", claim.Resource, "lease_token", claim.Token)
	}
}
