package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

type cdekStub struct {
	configured bool
	called     bool
	box        integration.Parcel
}

func (stub *cdekStub) Configured() bool { return stub.configured }

func (stub *cdekStub) FindCities(context.Context, string) ([]integration.CDEKCity, error) {
	stub.called = true
	return nil, nil
}

func (stub *cdekStub) GetOffices(context.Context, int) ([]integration.CDEKOffice, error) {
	stub.called = true
	return nil, nil
}

func (stub *cdekStub) CalculatePVZ(
	_ context.Context,
	_ int,
	box integration.Parcel,
) ([]integration.CDEKQuote, error) {
	stub.called = true
	stub.box = box
	return []integration.CDEKQuote{{TariffCode: 136, Price: 450}}, nil
}

// The checkout asks this before drawing the delivery options, so it can hide
// pick-up points instead of letting someone choose a method that cannot be
// completed — which is exactly what happened when the keys went missing.
func TestCDEKStatusReportsWhetherKeysAreConfigured(t *testing.T) {
	t.Parallel()

	for _, configured := range []bool{true, false} {
		dependencies := testDependencies(catalogStub{}, authStub{})
		dependencies.CDEK = &cdekStub{configured: configured}
		request := httptest.NewRequest(http.MethodGet, "/api/v1/delivery/cdek?action=status", nil)
		response := httptest.NewRecorder()

		NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
		want := `"available":true`
		if !configured {
			want = `"available":false`
		}
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body = %s, want %s", response.Body, want)
		}
	}
}

func TestCDEKRefusesWorkWithoutKeys(t *testing.T) {
	t.Parallel()

	stub := &cdekStub{configured: false}
	dependencies := testDependencies(catalogStub{}, authStub{})
	dependencies.CDEK = stub

	for _, target := range []string{
		"/api/v1/delivery/cdek?action=cities&city=Рязань",
		"/api/v1/delivery/cdek?action=offices&cityCode=44",
	} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		response := httptest.NewRecorder()
		NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, want %d", target, response.Code, http.StatusServiceUnavailable)
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/delivery/cdek",
		strings.NewReader(`{"cityCode":44,"items":[{"id":"ficus","quantity":1}]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("расчёт: status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}

	if stub.called {
		t.Fatal("СДЭК не должен вызываться без ключей")
	}
}

// The price follows the box, and the box follows the products in the cart.
// Quoting every order at one hardcoded size was charging Ryazan-to-Moscow
// customers over 1300 roubles for a parcel that costs a third of that.
func TestCDEKQuoteUsesTheBoxOfTheProductsInTheCart(t *testing.T) {
	t.Parallel()

	stub := &cdekStub{configured: true}
	dependencies := testDependencies(catalogStub{}, authStub{})
	dependencies.CDEK = stub
	dependencies.Packages = packageStub{
		"pineapple": {LengthCM: 40, WidthCM: 20, HeightCM: 20, WeightGrams: 1200},
		"monstera":  {LengthCM: 60, WidthCM: 20, HeightCM: 20, WeightGrams: 2300},
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/delivery/cdek", strings.NewReader(
		`{"cityCode":44,"items":[{"id":"pineapple","quantity":1},{"id":"monstera","quantity":1}]}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	want := integration.Parcel{LengthCM: 60, WidthCM: 40, HeightCM: 20, WeightGrams: 3500}
	if stub.box != want {
		t.Fatalf("box = %+v, want %+v", stub.box, want)
	}
	if !strings.Contains(response.Body.String(), `"quotes"`) {
		t.Fatalf("ответ без списка тарифов: %s", response.Body)
	}
}

type packageStub map[string]catalog.PackageSize

func (stub packageStub) PackageSizes(
	_ context.Context,
	slugs []string,
) (map[string]catalog.PackageSize, error) {
	sizes := make(map[string]catalog.PackageSize, len(slugs))
	for _, slug := range slugs {
		if size, found := stub[slug]; found {
			sizes[slug] = size
		}
	}
	return sizes, nil
}

// A plant nobody has measured yet must not produce a made-up price. The
// order still goes through — the manager works the cost out and calls back.
func TestCDEKAsksTheManagerWhenABoxIsMissing(t *testing.T) {
	t.Parallel()

	stub := &cdekStub{configured: true}
	dependencies := testDependencies(catalogStub{}, authStub{})
	dependencies.CDEK = stub
	dependencies.Packages = packageStub{
		"monstera": {LengthCM: 60, WidthCM: 20, HeightCM: 20, WeightGrams: 2300},
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/delivery/cdek", strings.NewReader(
		`{"cityCode":44,"items":[{"id":"monstera","quantity":1},{"id":"unmeasured","quantity":1}]}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	if !strings.Contains(response.Body.String(), `"pending":true`) {
		t.Fatalf("ожидали расчёт менеджером, получили: %s", response.Body)
	}
	if stub.called {
		t.Fatal("СДЭК не нужно спрашивать про коробку, которой нет")
	}
}
