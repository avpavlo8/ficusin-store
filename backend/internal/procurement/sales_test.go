package procurement

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type salesStoreStub struct {
	replaced map[string][]SalesRecord
	states   map[string]string
}

func (stub *salesStoreStub) RefreshSiteSales(context.Context, time.Time, time.Time) (int, error) {
	stub.states["site"] = "ok"
	return 1, nil
}
func (stub *salesStoreStub) ReplaceSales(_ context.Context, channel string, _, _ time.Time, records []SalesRecord) (int, error) {
	stub.replaced[channel] = records
	stub.states[channel] = "ok"
	return len(records), nil
}
func (stub *salesStoreStub) MarkSalesSync(_ context.Context, channel, status string, _ error) error {
	stub.states[channel] = status
	return nil
}

type salesSourceStub struct{}

func (salesSourceStub) Configured(string) bool { return true }
func (salesSourceStub) FetchSales(_ context.Context, channel string, from, _ time.Time) ([]SalesRecord, error) {
	return []SalesRecord{{Date: from, ExternalID: channel + "-1", Units: 2}}, nil
}

func TestSalesWorkerRefreshesEveryAutomaticChannel(t *testing.T) {
	store := &salesStoreStub{replaced: map[string][]SalesRecord{}, states: map[string]string{}}
	worker := NewSalesWorker(store, salesSourceStub{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.now = func() time.Time { return time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC) }
	worker.run(context.Background())
	if store.states["site"] != "ok" || store.states["wb"] != "ok" || store.states["ozon"] != "ok" {
		t.Fatalf("states = %+v", store.states)
	}
	if len(store.replaced["wb"]) != 1 || len(store.replaced["ozon"]) != 1 {
		t.Fatalf("replaced = %+v", store.replaced)
	}
}

func TestNormalizeSalesRecordsAggregatesSameProductAndDay(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	items, err := normalizeSalesRecords([]SalesRecord{
		{Date: from.Add(time.Hour), ExternalID: "A", Units: 2, GrossRUB: 200},
		{Date: from.Add(2 * time.Hour), ExternalID: "A", Units: -1, GrossRUB: -100},
	}, from, from)
	if err != nil || len(items) != 1 || items[0].Units != 1 || items[0].GrossRUB != 100 {
		t.Fatalf("items = %+v, err = %v", items, err)
	}
}
