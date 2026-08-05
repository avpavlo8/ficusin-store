package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

type cdekStub struct {
	configured bool
	called     bool
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

func (stub *cdekStub) CalculatePVZ(context.Context, int, int) (integration.CDEKQuote, error) {
	stub.called = true
	return integration.CDEKQuote{}, nil
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
		strings.NewReader(`{"cityCode":44,"itemCount":1}`))
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
