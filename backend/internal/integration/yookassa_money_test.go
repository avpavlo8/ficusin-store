package integration

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"testing"
)

func TestPaymentRejectsInvalidMoneyBeforePOST(t *testing.T) {
	client := NewYooKassaClient("shop", "secret", false, 0, 1)
	client.httpClient.Transport = yooKassaRoundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid money reached provider")
		return nil, nil
	})
	for _, amount := range []float64{math.NaN(), math.Inf(1), 1.005, -1, 0, 1e14} {
		if _, err := client.CreatePayment(context.Background(), PaymentRequest{Amount: amount}); err == nil {
			t.Fatalf("accepted %v", amount)
		}
		if _, err := client.PrepareRefund("pay", amount, "key", PaymentRequest{}); err == nil {
			t.Fatalf("accepted refund %v", amount)
		}
	}
}
func TestPaymentRejectsInvalidProviderMoney(t *testing.T) {
	for _, amount := range []string{"NaN", "Infinity", "100.001", "1e2", "-1.00", ""} {
		var p yooKassaPayment
		p.Amount.Value = amount
		if _, err := p.toPayment(); err == nil {
			t.Fatalf("accepted %q", amount)
		}
	}
}
func TestRefundOneKopeckPayloadAndReceipt(t *testing.T) {
	client := NewYooKassaClient("shop", "secret", true, 0, 1)
	request, err := client.PrepareRefund("pay", 0.29, "key", PaymentRequest{Items: []PaymentItem{{Name: "Plant", Price: 0.29, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient.Transport = yooKassaRoundTrip(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["amount"].(map[string]any)["value"] != "0.29" {
			t.Fatalf("wrong cents: %v", body)
		}
		return yooKassaTestResponse(`{"id":"refund","payment_id":"pay","status":"succeeded","amount":{"value":"0.29","currency":"RUB"}}`), nil
	})
	result, err := client.CreateRefund(context.Background(), request)
	if err != nil || result.Amount != 0.29 {
		t.Fatalf("%+v %v", result, err)
	}
}
