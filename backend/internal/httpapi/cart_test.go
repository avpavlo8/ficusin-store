package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

type cartStoreStub struct {
	items map[string]int
	err   error
}

func (stub cartStoreStub) Load(context.Context, int64) (map[string]int, error) {
	return stub.items, stub.err
}
func (stub cartStoreStub) Save(context.Context, int64, map[string]int) error { return stub.err }
func (stub cartStoreStub) LoadGuest(context.Context, string) (map[string]int, error) {
	return stub.items, stub.err
}
func (stub cartStoreStub) SaveGuest(context.Context, string, map[string]int, time.Time) error {
	return stub.err
}

type cartProductsStub struct {
	products []catalog.CartProduct
	err      error
}

func (stub cartProductsStub) CartProducts(context.Context, []string) ([]catalog.CartProduct, error) {
	return stub.products, stub.err
}

func TestCartReturnsSelfContainedVariantLines(t *testing.T) {
	t.Parallel()
	handler := cartHandler(discardLogger(), authStub{}, cartStoreStub{items: map[string]int{"FIC-000123-L": 2}}, cartProductsStub{
		products: []catalog.CartProduct{{
			ID: "123", SKU: "FIC-000123-L", Name: "Фикус", VariantLabel: "Большой",
			Price: 2490, Image: "/photo.webp", Stock: 4, Available: true,
		}},
	}, true)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/cart", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		Items map[string]int       `json:"items"`
		Lines []catalog.CartProduct `json:"lines"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode cart: %v", err)
	}
	if body.Items["FIC-000123-L"] != 2 || len(body.Lines) != 1 || body.Lines[0].VariantLabel != "Большой" {
		t.Fatalf("unexpected cart response: %#v", body)
	}
}

func TestCartReportsMissingSKUWithoutPretendingToBeEmpty(t *testing.T) {
	t.Parallel()
	handler := cartHandler(discardLogger(), authStub{}, cartStoreStub{items: map[string]int{"removed-sku": 1}}, cartProductsStub{}, true)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/cart", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !json.Valid(response.Body.Bytes()) {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Missing []string              `json:"missingSkus"`
		Lines   []catalog.CartProduct `json:"lines"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Missing) != 1 || body.Missing[0] != "removed-sku" || len(body.Lines) != 1 || body.Lines[0].Available {
		t.Fatalf("missing SKU was not reported: %#v, %v", body, err)
	}
}

func TestCartProductFailureIsExplicit(t *testing.T) {
	t.Parallel()
	handler := cartHandler(discardLogger(), authStub{}, cartStoreStub{items: map[string]int{"FIC-1": 1}}, cartProductsStub{err: errors.New("database unavailable")}, true)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/cart", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}
