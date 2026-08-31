package procurement

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type wbMirrorStoreStub struct {
	claims   map[string]bool
	finished map[string]time.Duration
	products []ChannelProduct
	sales    []SalesRecord
	states   map[string]string
}

func (stub *wbMirrorStoreStub) ClaimWBSync(_ context.Context, resource string, _ time.Duration) (bool, error) {
	if !stub.claims[resource] {
		return false, nil
	}
	stub.claims[resource] = false
	return true, nil
}

func (stub *wbMirrorStoreStub) FinishWBSync(_ context.Context, resource string, _ int, next time.Duration, _ error) error {
	stub.finished[resource] = next
	return nil
}

func (stub *wbMirrorStoreStub) RememberChannelProducts(_ context.Context, _ string, items []ChannelProduct) error {
	stub.products = append([]ChannelProduct(nil), items...)
	return nil
}

func (stub *wbMirrorStoreStub) ReplaceSales(_ context.Context, _ string, _, _ time.Time, items []SalesRecord) (int, error) {
	stub.sales = append([]SalesRecord(nil), items...)
	return len(items), nil
}

func (stub *wbMirrorStoreStub) MarkSalesSync(_ context.Context, channel, state string, _ error) error {
	stub.states[channel] = state
	return nil
}

type wbMirrorSourceStub struct {
	catalogCalls int
	salesCalls   int
	salesFrom    time.Time
	salesTo      time.Time
	err          error
}

func (*wbMirrorSourceStub) Configured(string) bool { return true }

func (stub *wbMirrorSourceStub) FetchCatalog(context.Context, string) ([]ChannelProduct, error) {
	stub.catalogCalls++
	return []ChannelProduct{{ExternalID: "123", Article: "plant-123", Barcodes: []string{"46001"}}}, stub.err
}

func (stub *wbMirrorSourceStub) FetchSales(_ context.Context, _ string, from, to time.Time) ([]SalesRecord, error) {
	stub.salesCalls++
	stub.salesFrom, stub.salesTo = from, to
	return []SalesRecord{{Date: to, ExternalID: "123", Units: 2}}, stub.err
}

func TestWBMirrorOwnsOneHourlyCatalogueAndSalesRefresh(t *testing.T) {
	store := &wbMirrorStoreStub{
		claims: map[string]bool{"catalog": true, "sales": true},
		finished: map[string]time.Duration{}, states: map[string]string{},
	}
	source := &wbMirrorSourceStub{}
	worker := NewWBMirrorWorker(store, source, slog.New(slog.NewTextHandler(io.Discard, nil)))
	moment := time.Date(2026, 8, 31, 10, 30, 0, 0, time.UTC)
	worker.now = func() time.Time { return moment }

	worker.run(context.Background())
	worker.run(context.Background()) // persistent claim says both lanes are no longer due

	if source.catalogCalls != 1 || source.salesCalls != 1 {
		t.Fatalf("remote calls: catalogue=%d sales=%d", source.catalogCalls, source.salesCalls)
	}
	if len(store.products) != 1 || len(store.sales) != 1 {
		t.Fatalf("mirror: products=%+v sales=%+v", store.products, store.sales)
	}
	if store.finished["catalog"] != time.Hour || store.finished["sales"] != time.Hour {
		t.Fatalf("next runs = %+v", store.finished)
	}
	if days := int(store.salesTo.Sub(store.salesFrom).Hours()/24) + 1; days != wbSalesDays {
		t.Fatalf("sales window = %d days, want %d", days, wbSalesDays)
	}
}

type retryAfterStub struct{ delay time.Duration }

func (err retryAfterStub) Error() string             { return "rate limited" }
func (err retryAfterStub) RetryDelay() time.Duration { return err.delay }

func TestWBMirrorPublishesExactRateLimitDelay(t *testing.T) {
	store := &wbMirrorStoreStub{
		claims: map[string]bool{"catalog": true}, finished: map[string]time.Duration{}, states: map[string]string{},
	}
	source := &wbMirrorSourceStub{err: fmtWrap(retryAfterStub{delay: 137 * time.Second})}
	worker := NewWBMirrorWorker(store, source, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.run(context.Background())
	if store.finished["catalog"] != 137*time.Second {
		t.Fatalf("retry = %v, want 137s", store.finished["catalog"])
	}
}

func fmtWrap(err error) error { return errors.Join(errors.New("Wildberries mirror"), err) }

