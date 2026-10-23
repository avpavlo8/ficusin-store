package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestRefundPreservesPendingAndExactReceipt(t *testing.T) {
	client := NewYooKassaClient("shop", "secret", 0, 1)
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
func TestFullRefundUsesProviderReceiptWithoutSecondComposition(t *testing.T) {
	client := NewYooKassaClient("fixture", "fixture", 0, 1)
	original := client.SnapshotReceipt(PaymentRequest{Amount: 100, Email: "fixture@example.invalid", Items: []PaymentItem{{Name: "Растение", Price: 100, Quantity: 1}}})
	req, err := client.PrepareRefund("pay-fixture", 100, "refund-fixture", original)
	if err != nil || req.Receipt != nil || !req.ReceiptByProvider {
		t.Fatalf("full fiscal refund: %+v %v", req, err)
	}
}
