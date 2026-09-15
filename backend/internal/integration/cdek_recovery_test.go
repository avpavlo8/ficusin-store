package integration

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type cdekRoundTripFunc func(*http.Request) (*http.Response, error)

func (function cdekRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestCDEKFindOrderByMerchantNumberIsReadOnly(t *testing.T) {
	client := NewCDEKClient("client", "secret")
	client.token = "test-token"
	client.tokenExpiry = time.Now().Add(time.Hour)
	client.httpClient = &http.Client{Transport: cdekRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet {
			t.Fatalf("method=%s, want GET", request.Method)
		}
		if request.URL.Path != "/v2/orders" || request.URL.Query().Get("im_number") != "CRM 13/1" {
			t.Fatalf("unexpected lookup URL: %s", request.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{"entity":{"uuid":"uuid-13","cdek_number":"track-13","statuses":[{"code":"CREATED","name":"Created"}]}}`)),
			Header: make(http.Header),
		}, nil
	})}

	shipment, err := client.FindOrderByNumber(context.Background(), "CRM 13/1")
	if err != nil {
		t.Fatal(err)
	}
	if shipment.UUID != "uuid-13" || shipment.TrackNumber != "track-13" || shipment.Status != "CREATED" {
		t.Fatalf("unexpected shipment: %+v", shipment)
	}
}

func TestCDEKServerFailureMakesCreateOutcomeUnknown(t *testing.T) {
	client := NewCDEKClient("client", "secret")
	client.token = "test-token"
	client.tokenExpiry = time.Now().Add(time.Hour)
	client.httpClient = &http.Client{Transport: cdekRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader(`{"message":"upstream failed"}`)),
			Header:     make(http.Header),
		}, nil
	})}

	_, err := client.CreateOrder(context.Background(), ShipmentRequest{
		OrderNumber: "CRM-13",
		TariffCode:  136,
		OfficeCode:  "PVZ-13",
		Box:         Parcel{LengthCM: 10, WidthCM: 10, HeightCM: 10, WeightGrams: 100},
	})
	if !errors.Is(err, ErrCDEKOutcomeUnknown) {
		t.Fatalf("error=%v, want ErrCDEKOutcomeUnknown", err)
	}
}
