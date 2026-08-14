package procurement

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type windowSourceStub struct {
	days []int
}

func (*windowSourceStub) Configured(string) bool { return true }

func (stub *windowSourceStub) FetchSales(_ context.Context, channel string, from, to time.Time) ([]SalesRecord, error) {
	if channel == "wb" {
		stub.days = append(stub.days, int(to.Sub(from).Hours()/24)+1)
	}
	return []SalesRecord{{Date: to, ExternalID: channel + "-1", Units: 1}}, nil
}

// Год целиком каждые сутки — двенадцать окон по месяцу и шесть тысяч
// отправлений ради нескольких новых строк. Ежедневно спрашиваем месяц,
// раз в неделю — год, чтобы подобрать возвраты и пропущенные дни.
func TestSalesWorkerAsksMonthDailyAndYearWeekly(t *testing.T) {
	store := &salesStoreStub{replaced: map[string][]SalesRecord{}, states: map[string]string{}}
	source := &windowSourceStub{}
	worker := NewSalesWorker(store, source, slog.New(slog.NewTextHandler(io.Discard, nil)))
	moment := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	worker.now = func() time.Time { return moment }

	worker.run(context.Background())
	moment = moment.AddDate(0, 0, 1)
	worker.run(context.Background())
	moment = moment.AddDate(0, 0, 1)
	worker.run(context.Background())
	moment = moment.AddDate(0, 0, 6)
	worker.run(context.Background())

	expected := []int{salesHistoryDays, salesRefreshDays, salesRefreshDays, salesHistoryDays}
	if len(source.days) != len(expected) {
		t.Fatalf("заходов = %v, ожидалось %v", source.days, expected)
	}
	for index, days := range expected {
		if source.days[index] != days {
			t.Fatalf("заход %d — окно %d дней, ожидалось %d: %v",
				index+1, source.days[index], days, source.days)
		}
	}
}
