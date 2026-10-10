package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type wbLimiterStub struct {
	reserved []string
	deferred map[string]time.Duration
	delay    time.Duration
}

func (stub *wbLimiterStub) ReserveWBRequest(_ context.Context, bucket string, _ time.Duration) (time.Duration, error) {
	stub.reserved = append(stub.reserved, bucket)
	return 0, nil
}

func (stub *wbLimiterStub) WBRequestDelay(_ context.Context, _ string) (time.Duration, error) {
	return stub.delay, nil
}

func TestWildberriesLongCooldownReleasesWorkerWithoutRequest(t *testing.T) {
	called := false
	limiter := &wbLimiterStub{delay: 90 * time.Minute}
	executor := NewMarketplaceExecutor("token", "", "").WithWBRequestLimiter(limiter)
	executor.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("request should not be sent during cooldown")
	})}
	_, err := executor.FetchSales(context.Background(), "wb", time.Now().AddDate(0, 0, -30), time.Now())
	var retryable interface{ RetryDelay() time.Duration }
	if !errors.As(err, &retryable) || retryable.RetryDelay() != 90*time.Minute {
		t.Fatalf("retry error = %v", err)
	}
	var diagnostic interface{ RateLimitCause() string }
	if !errors.As(err, &diagnostic) || diagnostic.RateLimitCause() != "общая пауза API: /api/v1/supplier/sales не запрашивался" {
		t.Fatalf("cooldown cause = %v", err)
	}
	if called || len(limiter.reserved) != 0 {
		t.Fatalf("cooldown sent a request or reserved a slot: called=%v slots=%v", called, limiter.reserved)
	}
}

func (stub *wbLimiterStub) DeferWBRequests(_ context.Context, bucket string, delay time.Duration) error {
	stub.deferred[bucket] = delay
	return nil
}

func TestOzonPriceIsConfirmedPerProduct(t *testing.T) {
	imports := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Client-Id") != "client" || request.Header.Get("Api-Key") != "secret" {
			t.Fatal("Ozon credentials are missing")
		}
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v5/product/info/prices" {
			var query struct {
				Filter struct {
					OfferID []string `json:"offer_id"`
				} `json:"filter"`
			}
			if err := json.NewDecoder(request.Body).Decode(&query); err != nil || len(query.Filter.OfferID) != 1 || query.Filter.OfferID[0] != "OZ-1" {
				t.Fatalf("Ozon readback query=%+v err=%v", query, err)
			}
			_, _ = response.Write([]byte(`{"items":[{"offer_id":"OZ-1","price":{"price":"1490","marketing_seller_price":"1200"}}]}`))
			return
		}
		if request.URL.Path != "/v1/product/import/prices" {
			t.Fatalf("unexpected Ozon request: %s", request.URL.Path)
		}
		imports++
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		prices, ok := body["prices"].([]any)
		if !ok || len(prices) != 1 {
			t.Fatalf("unexpected Ozon price payload: %#v", body)
		}
		row := prices[0].(map[string]any)
		if row["offer_id"] != "OZ-1" {
			t.Fatalf("unexpected Ozon price payload: %#v", body)
		}
		if _, exists := row["min_price"]; exists {
			t.Fatalf("empty optional min_price must be omitted: %#v", row)
		}
		_, _ = response.Write([]byte(`{"result":[{"offer_id":"OZ-1","updated":true,"errors":[]}]}`))
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	result, err := executor.Execute(context.Background(), procurement.ActionItem{Channel: "ozon", ExternalArticle: "OZ-1", NewValue: 1490})
	if err != nil || result.Completed || result.ExternalOperationID != ozonPriceCheckOperation {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	result, err = executor.Execute(context.Background(), procurement.ActionItem{Channel: "ozon", ExternalArticle: "OZ-1", NewValue: 1490, ExternalOperationID: result.ExternalOperationID})
	if err != nil || !result.Completed || imports != 1 {
		t.Fatalf("readback result = %#v, imports=%d, err = %v", result, imports, err)
	}
}

func TestWBSalesIncludeSalesAndSubtractReturns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/supplier/sales" {
			t.Fatalf("unexpected WB report request: %s %s", request.Method, request.URL.Path)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`[
			{"nmId":123,"saleID":"S1","date":"2026-08-05T12:00:00Z","finishedPrice":1500},
			{"nmId":123,"saleID":"R1","date":"2026-08-06T12:00:00Z","finishedPrice":1500}
		]`))
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("token", "", "")
	executor.wbReportsBase, executor.client = server.URL, server.Client()
	records, err := executor.FetchSales(context.Background(), "wb", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC))
	if err != nil || len(records) != 2 || records[0].Units != 1 || records[1].Units != -1 {
		t.Fatalf("records = %+v, err = %v", records, err)
	}
}

func TestWBSalesInterpretUnzonedDatesAsMoscowAndSendExplicitWindowZone(t *testing.T) {
	reports := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got := request.URL.Query().Get("dateFrom"); got != "2026-08-01T03:00:00+03:00" {
			t.Errorf("dateFrom=%q, want Moscow time at the UTC window boundary", got)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`[
			{"nmId":123,"saleID":"S1","date":"2026-08-05T12:00:00","finishedPrice":1500},
			{"nmId":124,"saleID":"S2","date":"2026-08-05T12:00:00Z","finishedPrice":1200}
		]`))
	}))
	defer reports.Close()
	executor := NewMarketplaceExecutor("token", "", "")
	executor.wbReportsBase, executor.client = reports.URL, reports.Client()
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	records, err := executor.FetchSales(context.Background(), "wb", from, to)
	if err != nil || len(records) != 2 {
		t.Fatalf("records=%+v, err=%v", records, err)
	}
	if want := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC); !records[0].Date.Equal(want) {
		t.Errorf("Moscow sale date=%s, want %s", records[0].Date, want)
	}
	if want := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC); !records[1].Date.Equal(want) {
		t.Errorf("explicit UTC sale date=%s, want %s", records[1].Date, want)
	}
}

func TestWBSalesUseOnlyOperationalReport(t *testing.T) {
	calls := 0
	reports := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls++
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/supplier/sales" || request.URL.Query().Get("flag") != "0" {
			t.Fatalf("unexpected operational report request: %s %s", request.Method, request.URL.String())
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`[
			{"nmId":123,"saleID":"S1","date":"2026-08-05T12:00:00Z","finishedPrice":1500},
			{"nmId":123,"saleID":"R1","date":"2026-08-06T12:00:00Z","finishedPrice":1500}
		]`))
	}))
	defer reports.Close()
	executor := NewMarketplaceExecutor("token", "", "")
	executor.wbReportsBase = reports.URL
	records, err := executor.FetchSales(context.Background(), "wb", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC))
	if err != nil || len(records) != 2 || records[0].Units != 1 || records[1].Units != -1 {
		t.Fatalf("records = %+v, err = %v", records, err)
	}
	if calls != 1 {
		t.Fatalf("operational report calls = %d, want 1", calls)
	}
}

func TestWBSalesContinueFromLastChangeDateWhenPageIsFull(t *testing.T) {
	calls := 0
	reports := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls++
		response.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = response.Write([]byte(`[
				{"nmId":101,"saleID":"S1","srid":"SR1","date":"2026-08-05T12:00:00Z","lastChangeDate":"2026-08-05T13:00:00Z","finishedPrice":1000},
				{"nmId":102,"saleID":"S2","srid":"SR2","date":"2026-08-06T12:00:00Z","lastChangeDate":"2026-08-06T13:00:00Z","finishedPrice":1100}
			]`))
			return
		}
		if got := request.URL.Query().Get("dateFrom"); got != "2026-08-06T13:00:00Z" {
			t.Fatalf("second page dateFrom=%q", got)
		}
		_, _ = response.Write([]byte(`[
			{"nmId":103,"saleID":"S3","srid":"SR3","date":"2026-08-07T12:00:00Z","lastChangeDate":"2026-08-07T13:00:00Z","finishedPrice":1200}
		]`))
	}))
	defer reports.Close()
	executor := NewMarketplaceExecutor("token", "", "")
	executor.wbReportsBase, executor.client = reports.URL, reports.Client()
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	records, err := executor.fetchWBOperationalSalesPaged(context.Background(), from, to, 2)
	if err != nil || len(records) != 3 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
	if records[0].SourceEventID != "SR1" {
		t.Fatalf("source event=%q, want WB srid", records[0].SourceEventID)
	}
}

func TestWildberries429WithoutRetryHeaderUsesSafeMinuteWindow(t *testing.T) {
	limiter := &wbLimiterStub{deferred: map[string]time.Duration{}}
	executor := NewMarketplaceExecutor("token", "", "").WithWBRequestLimiter(limiter)
	executor.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       io.NopCloser(strings.NewReader(`{"status":429}`)),
			Header:     make(http.Header),
		}, nil
	})}
	_, err := executor.FetchSales(context.Background(), "wb", time.Now().AddDate(0, 0, -30), time.Now())
	var retryable interface{ RetryDelay() time.Duration }
	if !errors.As(err, &retryable) || retryable.RetryDelay() != 65*time.Second {
		t.Fatalf("retry error=%v delay=%v", err, retryable)
	}
	if limiter.deferred["sales"] != 65*time.Second {
		t.Fatalf("published delay=%v", limiter.deferred["sales"])
	}
}

func TestOzonSalesCombineFBSAndFBO(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v3/posting/fbs/list" {
			_, _ = response.Write([]byte(`{"result":{"postings":[{"created_at":"2026-08-05T12:00:00Z","status":"delivered","products":[{"offer_id":"OZ-1","quantity":2,"price":"1200"}]}]}}`))
			return
		}
		_, _ = response.Write([]byte(`{"result":[{"created_at":"2026-08-06T12:00:00Z","status":"delivered","products":[{"offer_id":"OZ-1","quantity":1,"price":"1200"}]}]}`))
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	records, err := executor.FetchSales(context.Background(), "ozon", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC))
	if err != nil || len(records) != 2 || records[0].ExternalID != "OZ-1" || records[1].Units != 1 {
		t.Fatalf("records = %+v, err = %v", records, err)
	}
}

// Аналитика Ozon отдаёт продажи по SKU и названию товара, а справочник
// связан по offer_id — коду продавца. Строки из аналитики связать не с чем,
// и раньше это выглядело как «загружено 1529, связано 0»: отказ, переодетый
// в успех. Пустой ответ по отправлениям должен быть честной ошибкой.
func TestOzonSalesReportEmptyPostingsAsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v1/analytics/data" {
			t.Fatal("аналитика больше не источник продаж: её идентификатор нечем связать")
		}
		_, _ = response.Write([]byte(`{"result":{"postings":[]}}`))
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	records, err := executor.FetchSales(context.Background(), "ozon", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatalf("ожидалась ошибка, получено %d записей", len(records))
	}
	if !strings.Contains(err.Error(), "отправлений") {
		t.Fatalf("ошибка = %v", err)
	}
}

func TestWBSubmissionIsCheckedBeforeCompletion(t *testing.T) {
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v2/upload/task" {
			uploads++
			_, _ = response.Write([]byte(`{"data":{"id":42,"alreadyExists":false},"error":false,"errorText":""}`))
			return
		}
		if request.URL.Path == "/api/v2/list/goods/filter" {
			if request.URL.Query().Get("filterNmID") != "123" {
				t.Fatalf("WB readback nmID=%q", request.URL.Query().Get("filterNmID"))
			}
			_, _ = response.Write([]byte(`{"data":{"listGoods":[{"nmID":123,"sizes":[{"discountedPrice":1990}]}]}}`))
			return
		}
		_, _ = response.Write([]byte(`{"data":{"uploadID":42,"status":3},"error":false,"errorText":""}`))
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("token", "", "")
	executor.wbBase, executor.client = server.URL, server.Client()
	first, err := executor.Execute(context.Background(), procurement.ActionItem{Channel: "wb", ExternalArticle: "123", NewValue: 1990})
	if err != nil || first.Completed || first.ExternalOperationID != "42" {
		t.Fatalf("submission = %#v, err = %v", first, err)
	}
	second, err := executor.Execute(context.Background(), procurement.ActionItem{Channel: "wb", ExternalArticle: "123", NewValue: 1990, ExternalOperationID: "42"})
	if err != nil || !second.Completed || uploads != 1 {
		t.Fatalf("confirmation = %#v, err = %v", second, err)
	}
}

func TestWBPriceMismatchIsNeverMarkedCompleteOrUploadedAgain(t *testing.T) {
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v2/upload/task":
			uploads++
			var payload struct {
				Data []struct {
					Price    int64 `json:"price"`
					Discount int64 `json:"discount"`
				} `json:"data"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || len(payload.Data) != 1 || payload.Data[0].Price != 2000 || payload.Data[0].Discount != 25 {
				t.Fatalf("WB payload=%+v err=%v", payload, err)
			}
			_, _ = response.Write([]byte(`{"data":{"id":42},"error":false}`))
		case "/api/v2/history/tasks":
			_, _ = response.Write([]byte(`{"data":{"status":3},"error":false}`))
		case "/api/v2/list/goods/filter":
			_, _ = response.Write([]byte(`{"data":{"listGoods":[{"nmID":123,"sizes":[{"discountedPrice":1500}]}]}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("token", "", "")
	executor.wbBase, executor.client = server.URL, server.Client()
	strike := 2000.0
	item := procurement.ActionItem{Channel: "wb", ExternalArticle: "123", NewValue: 1499, CompareAtValue: &strike}
	first, err := executor.Execute(context.Background(), item)
	if err != nil || first.Completed || first.ExternalOperationID != "42" {
		t.Fatalf("upload=%+v err=%v", first, err)
	}
	item.ExternalOperationID = first.ExternalOperationID
	item.Attempts = 2
	second, err := executor.Execute(context.Background(), item)
	if err != nil || second.Completed || second.RetryAfter == 0 || uploads != 1 {
		t.Fatalf("mismatch=%+v uploads=%d err=%v", second, uploads, err)
	}
	item.Attempts = priceCheckMaxAttempts
	final, err := executor.Execute(context.Background(), item)
	if err == nil || final.Completed || uploads != 1 || !strings.Contains(err.Error(), "1500") {
		t.Fatalf("final=%+v uploads=%d err=%v", final, uploads, err)
	}
}

func TestOzonAcceptedPriceWaitsForSellerPriceReadback(t *testing.T) {
	imports := 0
	price := "1500"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/product/import/prices":
			imports++
			_, _ = response.Write([]byte(`{"result":[{"offer_id":"OZ-1","updated":true}]}`))
		case "/v5/product/info/prices":
			_, _ = response.Write([]byte(`{"items":[{"offer_id":"OZ-1","price":{"price":"` + price + `","marketing_seller_price":"1200"}}]}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	item := procurement.ActionItem{Channel: "ozon", ExternalArticle: "OZ-1", NewValue: 1490}
	first, err := executor.Execute(context.Background(), item)
	if err != nil || first.Completed || first.ExternalOperationID != ozonPriceCheckOperation {
		t.Fatalf("import=%+v err=%v", first, err)
	}
	item.ExternalOperationID = first.ExternalOperationID
	item.Attempts = 2
	second, err := executor.Execute(context.Background(), item)
	if err != nil || second.Completed || second.RetryAfter == 0 || imports != 1 {
		t.Fatalf("mismatch=%+v imports=%d err=%v", second, imports, err)
	}
	price = "1490"
	third, err := executor.Execute(context.Background(), item)
	if err != nil || !third.Completed || imports != 1 {
		t.Fatalf("readback=%+v imports=%d err=%v", third, imports, err)
	}
}

func TestOzonAmbiguousUploadDoesNotSendPriceTwice(t *testing.T) {
	imports := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/product/import/prices":
			imports++
			http.Error(response, `{"message":"gateway failure"}`, http.StatusBadGateway)
		case "/v5/product/info/prices":
			_, _ = response.Write([]byte(`{"items":[{"offer_id":"OZ-1","price":{"price":"1490"}}]}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	item := procurement.ActionItem{Channel: "ozon", ExternalArticle: "OZ-1", NewValue: 1490}
	first, err := executor.Execute(context.Background(), item)
	if err != nil || first.Completed || first.ExternalOperationID != ozonPriceCheckOperation {
		t.Fatalf("ambiguous upload=%+v err=%v", first, err)
	}
	item.ExternalOperationID = first.ExternalOperationID
	second, err := executor.Execute(context.Background(), item)
	if err != nil || !second.Completed || imports != 1 {
		t.Fatalf("readback=%+v imports=%d err=%v", second, imports, err)
	}
}

func TestWBRejectsSellerArticleInsteadOfNmID(t *testing.T) {
	executor := NewMarketplaceExecutor("token", "", "")
	_, err := executor.Execute(context.Background(), procurement.ActionItem{Channel: "wb", ExternalArticle: "X123", NewValue: 1000})
	if err == nil {
		t.Fatal("non-numeric WB article must not be sent as nmID")
	}
}

func TestMarketplaceProbesAreReadOnly(t *testing.T) {
	pingCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/ping":
			if request.Method != http.MethodGet {
				t.Fatalf("WB probe method = %s", request.Method)
			}
			pingCalls++
			_, _ = response.Write([]byte(`{"Status":"OK"}`))
		case "/v3/product/list":
			if request.Method != http.MethodPost {
				t.Fatalf("Ozon probe method = %s", request.Method)
			}
			_, _ = response.Write([]byte(`{"result":{"items":[]}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("token", "client", "secret")
	executor.wbBase, executor.wbReportsBase, executor.ozonBase, executor.client = server.URL, server.URL, server.URL, server.Client()
	for _, channel := range []string{"wb", "ozon"} {
		if err := executor.Probe(context.Background(), channel); err != nil {
			t.Fatalf("probe %s: %v", channel, err)
		}
	}
	if pingCalls != 2 {
		t.Fatalf("WB price and statistics pings = %d, want 2", pingCalls)
	}
}

func TestWBCatalogIncludesCurrentPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/content/v2/get/cards/list":
			_, _ = response.Write([]byte(`{"cards":[{"nmID":123,"vendorCode":"orchid-12","title":"Орхидея D12","sizes":[{"skus":["46001"]}]}],"cursor":{"total":1}}`))
		case "/api/v2/list/goods/filter":
			_, _ = response.Write([]byte(`{"data":{"listGoods":[{"nmID":123,"sizes":[{"price":3841,"discountedPrice":3201}]}]}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("token", "", "")
	executor.wbContentBase, executor.wbBase, executor.client = server.URL, server.URL, server.Client()
	items, err := executor.FetchCatalog(context.Background(), "wb")
	if err != nil || len(items) != 1 || items[0].CurrentPrice == nil || *items[0].CurrentPrice != 3201 {
		t.Fatalf("items = %#v, err = %v", items, err)
	}
}

func TestOzonCatalogIncludesCurrentPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v3/product/list":
			_, _ = response.Write([]byte(`{"result":{"items":[{"offer_id":"orchid-12"}],"last_id":""}}`))
		case "/v3/product/info/list":
			_, _ = response.Write([]byte(`{"items":[{"offer_id":"orchid-12","name":"Орхидея D12","barcodes":["46001"]}]}`))
		case "/v5/product/info/prices":
			_, _ = response.Write([]byte(`{"items":[{"offer_id":"orchid-12","price":{"price":"3201","marketing_seller_price":"2800","old_price":"3841"}}],"cursor":""}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	items, err := executor.FetchCatalog(context.Background(), "ozon")
	if err != nil || len(items) != 1 || items[0].CurrentPrice == nil || *items[0].CurrentPrice != 3201 {
		t.Fatalf("items = %#v, err = %v", items, err)
	}
}

func TestReadOnlyMarketplaceRequestRetriesRateLimit(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls++
		response.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			response.Header().Set("Retry-After", "1")
			response.WriteHeader(http.StatusTooManyRequests)
			_, _ = response.Write([]byte(`{"title":"too many requests"}`))
			return
		}
		_, _ = response.Write([]byte(`{"result":{"items":[]}}`))
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	if err := executor.Probe(context.Background(), "ozon"); err != nil {
		t.Fatalf("probe after rate limit: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestMarketplaceRetryAfterAcceptsWildberriesDecimalSeconds(t *testing.T) {
	response := &http.Response{Header: http.Header{"X-Ratelimit-Retry": []string{"2.5s"}}}
	if got := marketplaceRetryAfter(response); got != 2500*time.Millisecond {
		t.Fatalf("retry delay = %v, want 2.5s", got)
	}
}

func TestMarketplaceRetryAfterPrefersWildberriesHeaderOverMalformedGenericHeader(t *testing.T) {
	response := &http.Response{Header: http.Header{
		"X-Ratelimit-Retry": []string{"3600"},
		"Retry-After":       []string{"invalid"},
	}}
	if got := marketplaceRetryAfter(response); got != time.Hour {
		t.Fatalf("retry delay = %v, want one hour from WB header", got)
	}
}

func TestWildberries429StopsImmediatelyAndPublishesRetryWindow(t *testing.T) {
	calls := 0
	limiter := &wbLimiterStub{deferred: map[string]time.Duration{}}
	executor := NewMarketplaceExecutor("token", "", "").WithWBRequestLimiter(limiter)
	executor.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header: http.Header{
				"X-Ratelimit-Retry": []string{"137"},
				"X-Ratelimit-Limit": []string{"1"},
				"X-Ratelimit-Reset": []string{"138"},
			},
			Body: io.NopCloser(strings.NewReader(`{"status":429,"details":"rate limit exceeded","requestId":"a7b81234abcd5678"}`)),
		}, nil
	})}
	_, err := executor.FetchSales(context.Background(), "wb", time.Now().AddDate(0, 0, -30), time.Now())
	if err == nil {
		t.Fatal("expected rate limit")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want exactly one", calls)
	}
	var retryable interface{ RetryDelay() time.Duration }
	if !errors.As(err, &retryable) || retryable.RetryDelay() != 137*time.Second {
		t.Fatalf("retry error = %v", err)
	}
	var diagnostic interface{ RateLimitCause() string }
	if !errors.As(err, &diagnostic) || diagnostic.RateLimitCause() != "ответ 429 от /api/v1/supplier/sales; ID запроса WB a7b81234abcd5678; лимит 1; сброс через 2m18s" {
		t.Fatalf("remote rate limit cause = %v", err)
	}
	if limiter.deferred["sales"] != 137*time.Second {
		t.Fatalf("published delay = %v", limiter.deferred["sales"])
	}
}

func TestWildberries429DiagnosticRejectsUntrustedIdentifiers(t *testing.T) {
	for _, body := range []string{
		`{"requestId":"token=secret"}`,
		`{"requestId":"https://example.invalid/key"}`,
		`{"requestId":"short"}`,
		`not json`,
	} {
		if got := wbRateLimitRequestID([]byte(body)); got != "" {
			t.Fatalf("untrusted request ID %q reached operator message: %q", body, got)
		}
	}
	for _, header := range []string{"-1", "not-a-number", "999999999"} {
		if got := positiveHeaderInt(header); got != 0 {
			t.Fatalf("invalid rate header %q = %d", header, got)
		}
	}
}

func TestReadOnlyMarketplaceRequestRetriesEmptyJSON(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls++
		response.Header().Set("Content-Type", "application/json")
		if calls > 1 {
			_, _ = response.Write([]byte(`{"result":{"items":[]}}`))
		}
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	if err := executor.Probe(context.Background(), "ozon"); err != nil {
		t.Fatalf("probe after empty response: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}
