package integration

import (
	"context"
	"net/http"
	"testing"
	"time"
)

type recordingRoundTripper struct{ calls int }

func (transport *recordingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls++
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}, nil
}

// Обход каталога шлёт десятки запросов подряд, поэтому паузу держит
// транспорт, а не вызывающий код.
func TestPacedTransportWaitsBetweenRequestsToTheSameHost(t *testing.T) {
	base := &recordingRoundTripper{}
	transport := newPacedTransport(base)
	moment := time.Unix(0, 0)
	slept := make([]time.Duration, 0, 4)
	transport.now = func() time.Time { return moment }
	transport.wait = func(_ context.Context, wait time.Duration) error {
		slept = append(slept, wait)
		moment = moment.Add(wait)
		return nil
	}

	request, err := http.NewRequest(http.MethodGet, "https://api-seller.ozon.ru/v3/product/list", nil)
	if err != nil {
		t.Fatalf("создать запрос: %v", err)
	}
	for index := 0; index < 3; index++ {
		if _, err := transport.RoundTrip(request); err != nil {
			t.Fatalf("запрос %d: %v", index, err)
		}
	}
	if base.calls != 3 {
		t.Fatalf("запросов до площадки = %d, ожидалось 3", base.calls)
	}
	if len(slept) != 2 {
		t.Fatalf("пауз = %d, ожидалось 2", len(slept))
	}
	for _, wait := range slept {
		if wait != 600*time.Millisecond {
			t.Fatalf("пауза = %v, ожидалось 600ms", wait)
		}
	}
}

// Чужой хост тормозить незачем: лимиты считают только площадки.
func TestPacedTransportDoesNotDelayOtherHosts(t *testing.T) {
	base := &recordingRoundTripper{}
	transport := newPacedTransport(base)
	transport.wait = func(context.Context, time.Duration) error {
		t.Fatal("пауза для постороннего хоста")
		return nil
	}
	request, err := http.NewRequest(http.MethodGet, "https://example.com/health", nil)
	if err != nil {
		t.Fatalf("создать запрос: %v", err)
	}
	for index := 0; index < 3; index++ {
		if _, err := transport.RoundTrip(request); err != nil {
			t.Fatalf("запрос %d: %v", index, err)
		}
	}
}

func TestPacedTransportCancelsWait(t *testing.T) {
	base := &recordingRoundTripper{}
	transport := newPacedTransport(base)
	transport.now = func() time.Time { return time.Unix(0, 0) }
	request, _ := http.NewRequest(http.MethodGet, "https://api-seller.ozon.ru/v3/product/list", nil)
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request = request.WithContext(ctx)
	if _, err := transport.RoundTrip(request); err == nil {
		t.Fatal("cancelled wait reached external transport")
	}
	if base.calls != 1 {
		t.Fatalf("external calls=%d, want 1", base.calls)
	}
}

func TestWildberriesPriceUpdatesUseGradualCadence(t *testing.T) {
	if got := marketplacePace("discounts-prices-api.wildberries.ru"); got != 10*time.Second {
		t.Fatalf("WB price pause = %v, want 10s", got)
	}
}
