package payment

import "testing"

func TestBalanceFromStateWaitsForManagerWhenAnythingUnknown(t *testing.T) {
	for _, state := range []orderMoneyState{
		{total: 1500, feePending: true, method: MethodOnline, status: "new"},
		{total: 1500, hasPreorder: true, method: MethodOnline, status: "new"},
	} {
		balance := balanceFromState(state)
		if balance.Ready {
			t.Fatalf("неполный заказ не должен быть доступен к оплате: %+v", balance)
		}
		if balance.Due != 1500 || balance.PaymentStatus != StatusPending {
			t.Fatalf("неверный баланс ожидания: %+v", balance)
		}
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
