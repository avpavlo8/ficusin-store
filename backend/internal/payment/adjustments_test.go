package payment

import "testing"

func TestBalanceFromStateWaitsForPlantsButNotDeliveryQuote(t *testing.T) {
	waiting := balanceFromState(orderMoneyState{total: 1500, hasPreorder: true, method: MethodOnline, status: "new"})
	if waiting.Ready || waiting.Due != 1500 || waiting.PaymentStatus != StatusPending {
		t.Fatalf("заказ с недоступными растениями не должен оплачиваться: %+v", waiting)
	}
	ready := balanceFromState(orderMoneyState{total: 1500, feePending: true, method: MethodOnline, status: "new"})
	if !ready.Ready || ready.Due != 1500 {
		t.Fatalf("справочная стоимость доставки не должна блокировать оплату растений: %+v", ready)
	}
}

func TestBalanceFromStateSupportsTopUp(t *testing.T) {
	balance := balanceFromState(orderMoneyState{
		total: 2100, paid: 1500, method: MethodOnline, status: "confirmed",
	})
	if !balance.Ready || balance.Due != 600 || balance.NetPaid != 1500 || balance.PaymentStatus != "partially_paid" {
		t.Fatalf("доплата рассчитана неверно: %+v", balance)
	}
}

func TestBalanceFromStateFindsPartialRefund(t *testing.T) {
	balance := balanceFromState(orderMoneyState{
		total: 900, paid: 1500, refunded: 600, method: MethodOnline, status: "confirmed",
	})
	if balance.Due != 0 || balance.NetPaid != 900 || balance.PaymentStatus != StatusPaid {
		t.Fatalf("частичный возврат должен оставить заказ оплаченным: %+v", balance)
	}
}

func TestBalanceFromStateFindsOverpaymentBeforeRefund(t *testing.T) {
	balance := balanceFromState(orderMoneyState{
		total: 900, paid: 1500, method: MethodOnline, status: "confirmed",
	})
	if balance.Overpaid != 600 || balance.Due != 0 || balance.PaymentStatus != "partially_paid" {
		t.Fatalf("переплата рассчитана неверно: %+v", balance)
	}
}
