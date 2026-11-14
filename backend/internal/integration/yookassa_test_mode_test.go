package integration

import (
	"encoding/json"
	"testing"
)

func TestYooKassaPaymentPreservesVerifiedTestMode(t *testing.T) {
	for _, value := range []string{"true", "false", "null"} {
		t.Run(value, func(t *testing.T) {
			var response yooKassaPayment
			raw := `{"id":"fixture","status":"succeeded","paid":true,"test":` + value + `,"amount":{"value":"100.01","currency":"RUB"}}`
			if err := json.Unmarshal([]byte(raw), &response); err != nil {
				t.Fatal(err)
			}
			payment, err := response.toPayment()
			if err != nil {
				t.Fatal(err)
			}
			if value == "null" {
				if payment.Test != nil {
					t.Fatal("missing test mode was invented")
				}
			} else if payment.Test == nil || *payment.Test != (value == "true") {
				t.Fatal("verified test mode was lost")
			}
		})
	}
}
