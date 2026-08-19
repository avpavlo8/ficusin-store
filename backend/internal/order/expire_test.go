package order

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

// Заглушка платёжного сервиса: говорит то, что ей велели.
type stubCanceller struct {
	err    error
	called int
	orders []int64
}

func (stub *stubCanceller) CancelPending(_ context.Context, orderID int64) error {
	stub.called++
	stub.orders = append(stub.orders, orderID)
	return stub.err
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Главное свойство правки: пока платёж не закрыт у провайдера, заказ
// отменять нельзя. Отказ ЮKassa означает, что покупатель или платит прямо
// сейчас, или уже заплатил, — вернуть товар на полку в этот момент значит
// продать его дважды.
//
// Пул базы здесь не нужен нарочно: если проверка когда-нибудь уедет ниже
// открытия транзакции, тест упадёт на nil-пуле и это будет видно.
func TestExpiryKeepsOrderWhileProviderRefusesToCancelPayment(t *testing.T) {
	refusal := errors.New("ЮKassa не отменила платёж: succeeded")
	payments := &stubCanceller{err: refusal}
	worker := &ExpiryWorker{payments: payments, logger: quietLogger()}

	err := worker.cancel(context.Background(), 42)
	if err == nil {
		t.Fatal("заказ отменён, хотя платёж закрыть не удалось")
	}
	if !errors.Is(err, refusal) {
		t.Fatalf("потеряна причина отказа: %v", err)
	}
	if payments.called != 1 {
		t.Fatalf("платёж пытались закрыть %d раз, ожидали один", payments.called)
	}
	if len(payments.orders) != 1 || payments.orders[0] != 42 {
		t.Fatalf("закрывали платёж не того заказа: %v", payments.orders)
	}
}

// Старый конструктор брал три аргумента и о платежах не знал. Проверка
// закрепляет, что зависимость действительно доезжает до работника: без неё
// автоотмена вернётся к тихому поведению.
func TestNewExpiryWorkerKeepsPaymentCanceller(t *testing.T) {
	payments := &stubCanceller{}
	worker := NewExpiryWorker(nil, nil, payments, quietLogger())
	if worker.payments == nil {
		t.Fatal("автоотмена осталась без платёжного сервиса")
	}
}
