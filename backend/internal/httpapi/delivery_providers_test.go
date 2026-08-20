package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

type deliveryPricerStub struct {
	configured bool
	quote      integration.DeliveryQuote
	err        error
	called     bool
	address    string
	parcel     integration.Parcel
}

func (stub *deliveryPricerStub) Configured() bool { return stub.configured }
func (stub *deliveryPricerStub) Calculate(_ context.Context, address string, parcel integration.Parcel) (integration.DeliveryQuote, error) {
	stub.called = true
	stub.address = address
	stub.parcel = parcel
	return stub.quote, stub.err
}

func TestDeliveryProvidersExposeOnlyConfiguredServices(t *testing.T) {
	t.Parallel()
	dependencies := testDependencies(catalogStub{}, authStub{})
	dependencies.RussianPost = &deliveryPricerStub{configured: true}
	dependencies.YandexDelivery = &deliveryPricerStub{configured: false}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/delivery/providers", nil)
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	if !strings.Contains(body, `"post":true`) || !strings.Contains(body, `"courier":false`) {
		t.Fatalf("неверная доступность служб: %s", body)
	}
}

func TestPostQuoteUsesCombinedRealPackage(t *testing.T) {
	t.Parallel()
	post := &deliveryPricerStub{configured: true, quote: integration.DeliveryQuote{
		Price: 615, DaysMin: 2, DaysMax: 4, Service: "Почта России",
	}}
	dependencies := testDependencies(catalogStub{}, authStub{})
	dependencies.RussianPost = post
	dependencies.Packages = packageStub{
		"ficus":    {LengthCM: 30, WidthCM: 20, HeightCM: 40, WeightGrams: 1000},
		"monstera": {LengthCM: 50, WidthCM: 20, HeightCM: 60, WeightGrams: 2000},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/delivery/post", strings.NewReader(
		`{"address":"Москва, Мясницкая, 1","items":[{"id":"ficus","quantity":2},{"id":"monstera","quantity":1}]}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	if !post.called || post.address != "Москва, Мясницкая, 1" {
		t.Fatalf("Почта не получила адрес: called=%v address=%q", post.called, post.address)
	}
	want := integration.Parcel{LengthCM: 60, WidthCM: 70, HeightCM: 20, WeightGrams: 4000}
	if post.parcel != want {
		t.Fatalf("посылка = %+v, ожидали %+v", post.parcel, want)
	}
	if !strings.Contains(response.Body.String(), `"price":615`) {
		t.Fatalf("тариф не попал в ответ: %s", response.Body)
	}
}

func TestDeliveryQuoteDoesNotInventDimensions(t *testing.T) {
	t.Parallel()
	post := &deliveryPricerStub{configured: true, quote: integration.DeliveryQuote{Price: 615}}
	dependencies := testDependencies(catalogStub{}, authStub{})
	dependencies.RussianPost = post
	dependencies.Packages = packageStub{
		"ficus": {LengthCM: 30, WidthCM: 20, HeightCM: 40, WeightGrams: 1000},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/delivery/post", strings.NewReader(
		`{"address":"Москва, Мясницкая, 1","items":[{"id":"ficus","quantity":1},{"id":"unknown","quantity":1}]}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"pending":true`) {
		t.Fatalf("неизмеренная коробка должна уйти менеджеру: %d %s", response.Code, response.Body)
	}
	if post.called {
		t.Fatal("перевозчика спросили о выдуманной коробке")
	}
}

func TestDeliveryQuoteKeepsProviderDiagnosticsPrivate(t *testing.T) {
	t.Parallel()
	courier := &deliveryPricerStub{configured: true, err: errors.New("secret provider diagnostic")}
	dependencies := testDependencies(catalogStub{}, authStub{})
	dependencies.YandexDelivery = courier
	dependencies.Packages = packageStub{
		"ficus": {LengthCM: 30, WidthCM: 20, HeightCM: 40, WeightGrams: 1000},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/delivery/courier", strings.NewReader(
		`{"address":"Рязань, Ленина, 1","items":[{"id":"ficus","quantity":1}]}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"pending":true`) {
		t.Fatalf("сбой перевозчика должен оставить заказ возможным: %d %s", response.Code, response.Body)
	}
	if strings.Contains(response.Body.String(), "secret provider diagnostic") {
		t.Fatalf("диагностика провайдера утекла покупателю: %s", response.Body)
	}
}

// Keep this assertion close to the handlers: the catalog package type is the
// single source of dimensions for every carrier, not a separate delivery
// table with values that can drift.
var _ = catalog.PackageSize{}
