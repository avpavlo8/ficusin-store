package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestSettlementReceiptUsesOriginalTaxesAndExactPrepayment(t *testing.T) {
	original := NewYooKassaClient("fixture", "fixture", 2, 6).SnapshotReceipt(PaymentRequest{Email: "fixture@example.invalid", Items: []PaymentItem{{Name: "Растение", Price: 100.01, Quantity: 2}}})
	client := NewYooKassaClient("fixture", "fixture", 0, 1)
	req, err := client.PrepareSettlementReceipt("fixture-payment", "fixture-key", original, original.Items)
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient.Transport = yooKassaRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.Path != "/v3/receipts" || r.Header.Get("Idempotence-Key") != "fixture-key" {
			t.Fatal("wrong receipt endpoint or key")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		item := body["items"].([]any)[0].(map[string]any)
		settlement := body["settlements"].([]any)[0].(map[string]any)
		if body["tax_system_code"] != float64(2) || item["vat_code"] != float64(6) || item["payment_mode"] != "full_payment" || settlement["amount"].(map[string]any)["value"] != "200.02" {
			t.Fatalf("receipt drift: %+v", body)
		}
		return yooKassaTestResponse(`{"id":"fixture-receipt","payment_id":"fixture-payment","type":"payment","status":"pending"}`), nil
	})
	receipt, err := client.CreateSettlementReceipt(context.Background(), req)
	if err != nil || receipt.Status != "pending" {
		t.Fatalf("%+v %v", receipt, err)
	}
	if original.Items[0].Mode != "full_prepayment" {
		t.Fatal("original fiscal snapshot mutated")
	}
}
