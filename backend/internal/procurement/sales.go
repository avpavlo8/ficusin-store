package procurement

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

const salesHistoryDays = 365

type SalesStore interface {
	RefreshSiteSales(context.Context, time.Time, time.Time) (int, error)
	ReplaceSales(context.Context, string, time.Time, time.Time, []SalesRecord) (int, error)
	MarkSalesSync(context.Context, string, string, error) error
}

type SalesSource interface {
	Configured(string) bool
	FetchSales(context.Context, string, time.Time, time.Time) ([]SalesRecord, error)
}

type SalesWorker struct {
	store    SalesStore
	source   SalesSource
	logger   *slog.Logger
	interval time.Duration
	now      func() time.Time
}

func NewSalesWorker(store SalesStore, source SalesSource, logger *slog.Logger) *SalesWorker {
	return &SalesWorker{
		store: store, source: source, logger: logger,
		interval: 6 * time.Hour, now: time.Now,
	}
}

func (worker *SalesWorker) Run(ctx context.Context) {
	worker.run(ctx)
	ticker := time.NewTicker(worker.interval)
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

func (worker *SalesWorker) run(ctx context.Context) {
	to := day(worker.now().UTC())
	from := to.AddDate(0, 0, -(salesHistoryDays - 1))
	if err := worker.store.MarkSalesSync(ctx, "site", "running", nil); err == nil {
		if _, refreshErr := worker.store.RefreshSiteSales(ctx, from, to); refreshErr != nil {
			_ = worker.store.MarkSalesSync(ctx, "site", "error", refreshErr)
			worker.logger.Error("site sales synchronization failed", "error", refreshErr)
		}
	}
	for _, channel := range []string{"wb", "ozon"} {
		worker.syncExternal(ctx, channel, from, to)
	}
}

func (worker *SalesWorker) syncExternal(ctx context.Context, channel string, from, to time.Time) {
	if worker.source == nil || !worker.source.Configured(channel) {
		_ = worker.store.MarkSalesSync(ctx, channel, "disabled", nil)
		return
	}
	if err := worker.store.MarkSalesSync(ctx, channel, "running", nil); err != nil {
		worker.logger.Error("mark sales synchronization failed", "channel", channel, "error", err)
		return
	}
	records, err := worker.source.FetchSales(ctx, channel, from, to)
	if err == nil {
		_, err = worker.store.ReplaceSales(ctx, channel, from, to, records)
	}
	if err != nil {
		_ = worker.store.MarkSalesSync(ctx, channel, "error", err)
		worker.logger.Warn("marketplace sales synchronization failed", "channel", channel, "error", err)
	}
}

func day(value time.Time) time.Time {
	year, month, date := value.Date()
	return time.Date(year, month, date, 0, 0, 0, 0, time.UTC)
}

func validSalesChannel(value string) bool {
	return value == "site" || value == "saby" || value == "wb" || value == "ozon"
}

func normalizeSalesRecords(records []SalesRecord, from, to time.Time) ([]SalesRecord, error) {
	type key struct {
		date       string
		externalID string
	}
	aggregated := make(map[key]SalesRecord, len(records))
	for _, record := range records {
		record.Date = day(record.Date)
		record.ExternalID = strings.TrimSpace(record.ExternalID)
		record.SabyID = strings.TrimSpace(record.SabyID)
		if record.ExternalID == "" || record.Date.Before(from) || record.Date.After(to) {
			return nil, ErrInvalidInput
		}
		itemKey := key{date: record.Date.Format("2006-01-02"), externalID: record.ExternalID}
		item := aggregated[itemKey]
		item.Date, item.ExternalID = record.Date, record.ExternalID
		if record.SabyID != "" {
			item.SabyID = record.SabyID
		}
		item.Units += record.Units
		item.GrossRUB += record.GrossRUB
		aggregated[itemKey] = item
	}
	result := make([]SalesRecord, 0, len(aggregated))
	for _, record := range aggregated {
		if record.Units != 0 || record.GrossRUB != 0 {
			result = append(result, record)
		}
	}
	return result, nil
}
