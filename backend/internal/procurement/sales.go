package procurement

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

const salesHistoryDays = 365

// salesRefreshDays — глубина ежедневного обмена с площадками.
//
// Год целиком каждые сутки — это двенадцать окон по месяцу и шесть тысяч
// отправлений ради нескольких новых строк, и чем дольше живёт магазин,
// тем дороже этот заход. История уже лежит у нас в базе, а ReplaceSales
// чистит только тот период, который ей передали, — значит узкое окно
// ничего не теряет.
//
// Почему не «только вчера»: отправление становится доставленным не в день
// заказа, а через неделю-две, и его статус меняется задним числом.
// Суточное окно такие продажи не увидело бы никогда.
const salesRefreshDays = 30

// salesDeepEvery — как часто всё же переспрашивать год. Он нужен не ради
// свежих продаж, а ради починки: возвраты и корректировки площадки
// проводят задним числом, а день, когда обмен отказал, иначе остался бы
// пустым навсегда.
const salesDeepEvery = 7 * 24 * time.Hour

type SalesStore interface {
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
	// externalEvery — как часто спрашиваем сами площадки. Wildberries держит
	// лимит на продавца, общий на все его интеграции сразу, и четыре захода
	// в сутки мы тратили сами: отчёт отвечал 429 ещё до того, как им
	// воспользовался кто-то другой. Продажи за год меняются медленно, чаще
	// раза в день их спрашивать незачем.
	externalEvery time.Duration
	// externalAt — когда площадки ответили в последний раз. Отмечаем только
	// удачную попытку: неудачная должна повториться на ближайшем такте, а не
	// ждать сутки, иначе один отказ площадки съест весь день.
	externalAt map[string]time.Time
	// deepAt — когда в последний раз переспросили год целиком.
	deepAt map[string]time.Time
	now    func() time.Time
}

func NewSalesWorker(store SalesStore, source SalesSource, logger *slog.Logger) *SalesWorker {
	return &SalesWorker{
		store: store, source: source, logger: logger,
		interval: 6 * time.Hour, externalEvery: 24 * time.Hour,
		externalAt: map[string]time.Time{}, deepAt: map[string]time.Time{}, now: time.Now,
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
	// Сайт считается из своей же базы, никаких чужих лимитов не занимает,
	// поэтому пересчитывается на каждом такте и за весь год.
	if err := worker.store.MarkSalesSync(ctx, "site", "running", nil); err == nil {
		if _, refreshErr := worker.store.RefreshSiteSales(ctx, from, to); refreshErr != nil {
			_ = worker.store.MarkSalesSync(ctx, "site", "error", refreshErr)
			worker.logger.Error("site sales synchronization failed", "error", refreshErr)
		}
	}
	for _, channel := range []string{"wb", "ozon"} {
		if !worker.externalDue(channel) {
			continue
		}
		deep := worker.deepDue(channel)
		since := to.AddDate(0, 0, -(salesRefreshDays - 1))
		if deep {
			since = from
		}
		if !worker.syncExternal(ctx, channel, since, to) {
			continue
		}
		if deep {
			worker.deepAt[channel] = worker.now().UTC()
		}
	}
}

// externalDue отвечает, пора ли снова беспокоить площадку.
func (worker *SalesWorker) externalDue(channel string) bool {
	last, known := worker.externalAt[channel]
	return !known || worker.now().UTC().Sub(last) >= worker.externalEvery
}

// deepDue отвечает, пора ли переспросить всю историю. После перезапуска
// приложения память пуста, и первый заход всегда глубокий — так пустая
// база заполняется сама, без отдельного шага наливки.
func (worker *SalesWorker) deepDue(channel string) bool {
	last, known := worker.deepAt[channel]
	return !known || worker.now().UTC().Sub(last) >= salesDeepEvery
}

func (worker *SalesWorker) syncExternal(ctx context.Context, channel string, from, to time.Time) bool {
	if worker.source == nil || !worker.source.Configured(channel) {
		_ = worker.store.MarkSalesSync(ctx, channel, "disabled", nil)
		return false
	}
	if err := worker.store.MarkSalesSync(ctx, channel, "running", nil); err != nil {
		worker.logger.Error("mark sales synchronization failed", "channel", channel, "error", err)
		return false
	}
	records, err := worker.source.FetchSales(ctx, channel, from, to)
	if err != nil {
		// Отказ площадки попадает на плашку канала как есть, и короткой строки
		// там не хватало: «не вернул отправлений» одинаково звучит и когда
		// отправлений нет, и когда они есть, но без кода продавца.
		if reporter, able := worker.source.(SalesDiagnostics); able {
			if detailed := reporter.DescribeSalesFailure(ctx, channel, from, to, err); detailed != nil {
				err = detailed
			}
		}
	} else {
		_, err = worker.store.ReplaceSales(ctx, channel, from, to, records)
	}
	if err != nil {
		_ = worker.store.MarkSalesSync(ctx, channel, "error", err)
		worker.logger.Warn("marketplace sales synchronization failed", "channel", channel, "error", err)
		return false
	}
	worker.externalAt[channel] = worker.now().UTC()
	return true
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
