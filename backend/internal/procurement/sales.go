package procurement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const salesHistoryDays = 365

// salesRefreshDays — окно обычного задания синхронизации продаж.
//
// Полный год на каждом пятиминутном задании повторял бы дорогую загрузку
// ради нескольких новых строк. История уже лежит у нас в базе, а ReplaceSales
// заменяет только переданный период. PostgreSQL назначает обычные задания
// с этим окном и глубокую проверку года раз в неделю.
//
// Почему не «только вчера»: отправление становится доставленным не в день
// заказа, а через неделю-две, и его статус меняется задним числом.
// Суточное окно такие продажи не увидело бы никогда.
const salesRefreshDays = 30

var salesMoscow = time.FixedZone("Europe/Moscow", 3*60*60)

type SalesStore interface {
	IntegrationSyncCoordinator
	RefreshSiteSales(context.Context, time.Time, time.Time) (int, error)
	ReplaceSales(context.Context, string, time.Time, time.Time, []SalesRecord) (int, error)
	MarkSalesSync(context.Context, string, string, error) error
}

type SalesSource interface {
	Configured(string) bool
	FetchSales(context.Context, string, time.Time, time.Time) ([]SalesRecord, error)
}

// SalesDiagnostics — необязательное дополнение к источнику: умение объяснить
// собственный отказ. Сама выгрузка возвращает короткую ошибку — «площадка
// не вернула отправлений», — а за ней прячутся разные беды с разным лечением.
// Кто умеет рассказать подробнее — рассказывает, и это видно на плашке канала.
type SalesDiagnostics interface {
	DescribeSalesFailure(context.Context, string, time.Time, time.Time, error) error
}

type SalesWorker struct {
	store    SalesStore
	source   SalesSource
	logger   *slog.Logger
	interval time.Duration
	now      func() time.Time
	owner    string
	siteAt   time.Time
}

func NewSalesWorker(store SalesStore, source SalesSource, logger *slog.Logger) *SalesWorker {
	return &SalesWorker{
		store: store, source: source, logger: logger,
		interval: time.Minute, now: time.Now,
		owner: fmt.Sprintf("sales-%d", time.Now().UnixNano()),
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
	to := day(worker.now().In(salesMoscow))
	from := to.AddDate(0, 0, -(salesHistoryDays - 1))
	// Local sales do not consume an external limit, but a full-year aggregate
	// still should not run every minute while workers poll for due lanes.
	if worker.siteAt.IsZero() || worker.now().UTC().Sub(worker.siteAt) >= integrationCurrentEvery {
		if err := worker.store.MarkSalesSync(ctx, "site", "running", nil); err == nil {
			if _, refreshErr := worker.store.RefreshSiteSales(ctx, from, to); refreshErr != nil {
				_ = worker.store.MarkSalesSync(ctx, "site", "error", refreshErr)
				worker.logger.Error("site sales synchronization failed", "error", refreshErr)
			} else {
				worker.siteAt = worker.now().UTC()
			}
		}
	}
	for _, channel := range []string{"saby", "ozon"} {
		worker.runCoordinated(ctx, channel, from, to)
	}
}

func (worker *SalesWorker) runCoordinated(ctx context.Context, channel string, deepFrom, to time.Time) {
	if worker.source == nil || !worker.source.Configured(channel) {
		_ = worker.store.MarkSalesSync(ctx, channel, "disabled", nil)
		return
	}
	claim, err := worker.store.ClaimIntegrationSync(ctx, channel, "sales", worker.owner, 20*time.Minute)
	if err != nil {
		worker.logger.Error("claim marketplace sales synchronization failed", "channel", channel, "error", err)
		return
	}
	if claim == nil {
		return
	}
	from := to.AddDate(0, 0, -(salesRefreshDays - 1))
	if claim.Mode == "deep" {
		from = deepFrom
	}
	_ = worker.store.MarkSalesSync(ctx, channel, "running", nil)
	records, syncErr := worker.source.FetchSales(ctx, channel, from, to)
	if syncErr != nil {
		if reporter, able := worker.source.(SalesDiagnostics); able {
			if detailed := reporter.DescribeSalesFailure(ctx, channel, from, to, syncErr); detailed != nil {
				syncErr = detailed
			}
		}
	} else {
		_, syncErr = worker.store.ReplaceSales(ctx, channel, from, to, records)
	}
	latest := latestSalesEvent(records)
	retry := retryDelay(syncErr)
	applied, finishErr := worker.store.FinishIntegrationSync(ctx, *claim, len(records), from, to, latest, retry, syncErr)
	if finishErr != nil {
		worker.logger.Error("finish marketplace sales synchronization failed", "channel", channel, "error", finishErr)
		return
	}
	if !applied {
		worker.logger.Warn("ignored stale marketplace sales finisher", "channel", channel, "lease_token", claim.Token)
		return
	}
	if syncErr != nil {
		_ = worker.store.MarkSalesSync(ctx, channel, "error", syncErr)
		worker.logger.Warn("marketplace sales synchronization failed", "channel", channel, "error", syncErr)
	}
}

func latestSalesEvent(records []SalesRecord) *time.Time {
	var latest time.Time
	for _, record := range records {
		if record.Date.After(latest) {
			latest = record.Date
		}
	}
	if latest.IsZero() {
		return nil
	}
	return &latest
}

func retryDelay(err error) time.Duration {
	if err == nil {
		return 0
	}
	var retryable interface{ RetryDelay() time.Duration }
	if errors.As(err, &retryable) {
		return retryable.RetryDelay()
	}
	return 0
}

func day(value time.Time) time.Time {
	year, month, date := value.Date()
	return time.Date(year, month, date, 0, 0, 0, 0, time.UTC)
}

func validSalesChannel(value string) bool {
	return value == "site" || value == "saby" || value == "wb" || value == "ozon"
}
