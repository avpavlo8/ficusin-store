package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type yooKassaRoundTrip func(*http.Request) (*http.Response, error)

func (fn yooKassaRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func yooKassaTestResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestYooKassaCreatePaymentUsesTwoStages(t *testing.T) {
	client := NewYooKassaClient("shop", "secret", false, 0, 1)
	client.httpClient.Transport = yooKassaRoundTrip(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/v3/payments" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["capture"] != false {
			t.Fatalf("capture = %v, want false", body["capture"])
		}
		return yooKassaTestResponse(`{"id":"pay-1","status":"pending","paid":false,"amount":{"value":"1000.00"},"confirmation":{"confirmation_url":"https://example.test/pay"}}`), nil
	})
	payment, err := client.CreatePayment(context.Background(), PaymentRequest{IdempotenceKey: "attempt-1", Amount: 1000, ReturnURL: "https://example.test/return"})
	if err != nil || payment.ID != "pay-1" || payment.Status != "pending" {
		t.Fatalf("payment = %+v, err = %v", payment, err)
	}
}

func TestYooKassaCaptureAndFetchWaitingForCapture(t *testing.T) {
	client := NewYooKassaClient("shop", "secret", false, 0, 1)
	client.httpClient.Transport = yooKassaRoundTrip(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v3/payments/pay-1":
			return yooKassaTestResponse(`{"id":"pay-1","status":"waiting_for_capture","paid":true,"amount":{"value":"1000.00"}}`), nil
		case request.Method == http.MethodPost && request.URL.Path == "/v3/payments/pay-1/capture":
			if request.Header.Get("Idempotence-Key") != "capture-attempt-1" {
				t.Fatalf("wrong capture idempotence key: %q", request.Header.Get("Idempotence-Key"))
			}
			body, err := io.ReadAll(request.Body)
			if err != nil || len(body) != 0 {
				t.Fatalf("capture must have an empty body: %q %v", body, err)
			}
			return yooKassaTestResponse(`{"id":"pay-1","status":"succeeded","paid":true,"amount":{"value":"1000.00"}}`), nil
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
			return nil, nil
		}
	})
	payment, err := client.FetchPayment(context.Background(), "pay-1")
	if err != nil || payment.Status != "waiting_for_capture" || !payment.Paid {
		t.Fatalf("payment = %+v, err = %v", payment, err)
	}
	if err := client.CapturePayment(context.Background(), "pay-1", "capture-attempt-1"); err != nil {
		t.Fatal(err)
	}
}

func TestYooKassaCaptureNeedsStableIdempotenceKey(t *testing.T) {
	client := NewYooKassaClient("shop", "secret", false, 0, 1)
	if err := client.CapturePayment(context.Background(), "pay-1", ""); err == nil {
		t.Fatal("capture without idempotence key must fail")
	}
}
