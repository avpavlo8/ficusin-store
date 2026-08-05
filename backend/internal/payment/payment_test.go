package payment

import "testing"

func methodIDs(methods []Method) string {
	ids := ""
	for _, method := range methods {
		ids += method.ID + " "
	}
	return ids
}

// Paying at the counter needs a counter. A parcel handed to CDEK is gone
// before anyone could collect the money for it.
func TestPayOnCollectionIsOfferedOnlyForSelfCollection(t *testing.T) {
	for delivery, wanted := range map[string]bool{
		"pickup":  true,
		"cdek":    false,
		"courier": false,
		"post":    false,
	} {
		offered := Allowed(MethodOnDelivery, delivery, false, true)
		if offered != wanted {
			t.Fatalf("%s: оплата при получении = %v, ожидали %v", delivery, offered, wanted)
		}
	}
}

// An invoice is paperwork for a company account. A retail customer who
// asked for one by editing the request does not get one.
func TestInvoiceIsOfferedOnlyToWholesale(t *testing.T) {
	if Allowed(MethodInvoice, "pickup", false, true) {
		t.Fatal("розничный покупатель не должен платить по счёту")
	}
	if !Allowed(MethodInvoice, "cdek", true, true) {
		t.Fatal("оптовому покупателю счёт должен быть доступен")
	}
}

// No keys means no card payment anywhere in the shop — the same rule the
// rest of the integrations follow.
func TestCardDisappearsWhenPaymentIsNotConfigured(t *testing.T) {
	methods := Methods("cdek", false, false)

	if len(methods) != 0 {
		t.Fatalf("без ключей вариантов быть не должно, получили: %s", methodIDs(methods))
	}
	if Allowed(MethodOnline, "cdek", false, false) {
		t.Fatal("оплата картой не должна проходить без ключей")
	}
}

func TestInitialStatusFollowsTheChosenMethod(t *testing.T) {
	for method, wanted := range map[string]string{
		MethodOnline:     StatusPending,
		MethodOnDelivery: StatusOnDelivery,
		MethodInvoice:    StatusInvoice,
	} {
		if status := InitialStatus(method); status != wanted {
			t.Fatalf("%s: статус = %s, ожидали %s", method, status, wanted)
		}
	}
}
