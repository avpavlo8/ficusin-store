package integration

import (
	"net/http"
	"testing"
	"time"
)

type recordingRoundTripper struct{ calls int }

func (transport *recordingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls++
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}, nil
}

// Ozon отвечает отказом на третий запрос в секунду, Wildberries считает
// лимит на продавца целиком. Обход каталога шлёт десятки запросов подряд,
// поэтому паузу держит транспорт, а не вызывающий код.
func TestPacedTransportWaitsBetweenRequestsToTheSameHost(t *testing.T) {
	base := &recordingRoundTripper{}
	transport := newPacedTransport(base)
	moment := time.Unix(0, 0)
	slept := make([]time.Duration, 0, 4)
	transport.now = func() time.Time { return moment }
	transport.sleep = func(wait time.Duration) {
		slept = append(slept, wait)
		moment = moment.Add(wait)
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
	transport.sleep = func(time.Duration) { t.Fatal("пауза для постороннего хоста") }
	request, err := http.NewRequest(http.MethodGet, "https://online.sbis.ru/oauth/service/", nil)
	if err != nil {
		t.Fatalf("создать запрос: %v", err)
	}
	for index := 0; index < 3; index++ {
		if _, err := transport.RoundTrip(request); err != nil {
			t.Fatalf("запрос %d: %v", index, err)
		}
	}
}

func TestWildberriesPriceUpdatesUseGradualCadence(t *testing.T) {
	if got := marketplacePace("discounts-prices-api.wildberries.ru"); got != 10*time.Second {
		t.Fatalf("WB price pause = %v, want 10s", got)
	}
}
