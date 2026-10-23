package order

import (
	"context"
	"testing"
)

func TestCreateRejectsRetiredDeliveryMethodsBeforeDatabaseAccess(t *testing.T) {
	service := &Service{}
	for _, method := range []string{"post", "courier"} {
		_, err := service.Create(context.Background(), CreateInput{Consent: true, OfferAccepted: true, Delivery: method})
		if err == nil || err.Error() != "Выберите доступный способ получения" {
			t.Errorf("method %q: expected unavailable delivery validation, got %v", method, err)
		}
	}
}

func TestOrderRequiresOfferIndependentlyOfPersonalDataConsent(t *testing.T) {
	service := &Service{}
	if _, err := service.Create(context.Background(), CreateInput{Consent: true, Delivery: "pickup"}); err == nil {
		t.Fatal("accepted consent without offer")
	}
}
