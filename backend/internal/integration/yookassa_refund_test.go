package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestRefundPreservesPendingAndExactReceipt(t *testing.T) {
	client := NewYooKassaClient("shop", "secret", true, 0, 1)
	original := PaymentRequest{Email: "test@example.invalid", Items: []PaymentItem{{Name: "Растение", Price: 100, Quantity: 1}}}
	req, err := client.PrepareRefund("pay-1", 100, "refund-key", original)
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient.Transport = yooKassaRoundTrip(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["receipt"] == nil || r.Header.Get("Idempotence-Key") != "refund-key" {
			t.Fatal("receipt or key missing")
		}
		return yooKassaTestResponse(`{"id":"ref-1","payment_id":"pay-1","status":"pending","amount":{"value":"100.00","currency":"RUB"}}`), nil
	})
	result, err := client.CreateRefund(context.Background(), req)
	if err != nil || result.Status != "pending" {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err = client.PrepareRefund("pay-1", 50, "other", original); err == nil {
		t.Fatal("partial refund invented a fiscal composition")
	}
}
func TestOwnTillRefundDoesNotSendSecondReceipt(t *testing.T) {
	client := NewYooKassaClient("shop", "secret", false, 0, 1)
	req, err := client.PrepareRefund("pay-1", 50, "key", PaymentRequest{})
	if err != nil || req.Receipt != nil {
		t.Fatalf("%+v %v", req, err)
	}
}
