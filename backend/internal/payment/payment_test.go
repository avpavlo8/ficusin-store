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

	if len(methods) != 1 || methods[0].ID != MethodManager {
		t.Fatalf("без ключей нужна оплата после подтверждения, получили: %s", methodIDs(methods))
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
		MethodManager:    StatusPending,
	} {
		if status := InitialStatus(method); status != wanted {
			t.Fatalf("%s: статус = %s, ожидали %s", method, status, wanted)
		}
	}
}

func TestPreorderKeepsAvailablePaymentChoice(t *testing.T) {
	for _, tc := range []struct {
		name, requested, delivery, want string
		online, preorder, allowed       bool
	}{
		{"pickup cash", MethodOnDelivery, "pickup", MethodOnDelivery, true, true, true},
		{"pickup cash without online", MethodOnDelivery, "pickup", MethodOnDelivery, false, true, true},
		{"preorder default", "", "pickup", MethodOnline, true, true, true},
		{"preorder without online", "", "pickup", MethodManager, false, true, true},
		{"regular CDEK rejects cash", MethodOnDelivery, "cdek", "", true, false, false},
		{"CDEK preorder uses future online", MethodOnDelivery, "cdek", MethodOnline, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, allowed := ResolveMethod(tc.requested, tc.delivery, false, tc.online, tc.preorder)
			if got != tc.want || allowed != tc.allowed {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, allowed, tc.want, tc.allowed)
			}
		})
	}
}
