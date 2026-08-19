package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
)

// Защита держится на приведении типа: платёжный сервис подставляется как
// refundService, а гасить платежи умеет через отдельный интерфейс. Если
// подпись метода уедет, приведение перестанет срабатывать молча — и
// молча же перестанет работать вся защита. Эта строка превращает такую
// поломку в ошибку компиляции.
var _ pendingPaymentCanceller = (*payment.Service)(nil)

type cancellingPaymentsStub struct {
	err       error
	cancelled []int64
}

func (stub *cancellingPaymentsStub) Refund(context.Context, int64) error { return nil }

func (stub *cancellingPaymentsStub) CancelPending(_ context.Context, orderID int64) error {
	stub.cancelled = append(stub.cancelled, orderID)
	return stub.err
}

// Отдельная обёртка над общей заглушкой: нужно считать, дошло ли дело до
// смены статуса заказа.
type orderStatusRepositoryStub struct {
	*adminRepositoryStub
	statusCalls int
}

func (stub *orderStatusRepositoryStub) UpdateOrderStatus(
	context.Context, admin.Actor, int64, string, string,
) (admin.Order, error) {
	stub.statusCalls++
	return admin.Order{ID: 7}, nil
}

func patchOrder(
	t *testing.T, payments refundService, body string,
) (*httptest.ResponseRecorder, *orderStatusRepositoryStub) {
	t.Helper()
	repository := &orderStatusRepositoryStub{adminRepositoryStub: &adminRepositoryStub{}}
	dependencies := adminDependencies(repository, admin.RoleOwner, "owner@example.com")
	dependencies.Refunds = payments
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), dependencies).ServeHTTP(
		response, adminRequest(http.MethodPatch, "/api/v1/admin/orders/7", body),
	)
	return response, repository
}

// Главное свойство: пока платёж не закрыт у провайдера, заказ отменять
// нельзя. Отказ ЮKassa означает, что покупатель или платит прямо сейчас,
// или уже заплатил — вернуть товар на полку в этот момент значит продать
// его дважды.
func TestManualCancelStopsWhenPaymentCannotBeClosed(t *testing.T) {
	t.Parallel()

	payments := &cancellingPaymentsStub{err: errors.New("ЮKassa не отменила платёж: succeeded")}
	response, repository := patchOrder(t, payments, `{"status":"cancelled"}`)

	if response.Code != http.StatusConflict {
		t.Fatalf("статус ответа %d, ожидали %d", response.Code, http.StatusConflict)
	}
	if repository.statusCalls != 0 {
		t.Fatal("заказ отменён, хотя платёж закрыть не удалось")
	}
	if len(payments.cancelled) != 1 || payments.cancelled[0] != 7 {
		t.Fatalf("закрывали платёж не того заказа: %v", payments.cancelled)
	}
}

func TestManualCancelClosesPaymentFirst(t *testing.T) {
	t.Parallel()

	payments := &cancellingPaymentsStub{}
	response, repository := patchOrder(t, payments, `{"status":"cancelled"}`)

	if response.Code != http.StatusOK {
		t.Fatalf("статус ответа %d, ожидали %d", response.Code, http.StatusOK)
	}
	if len(payments.cancelled) != 1 {
		t.Fatalf("платёж закрывали %d раз, ожидали один", len(payments.cancelled))
	}
	if repository.statusCalls != 1 {
		t.Fatalf("заказ обновляли %d раз, ожидали один", repository.statusCalls)
	}
}

// Остальные статусы платежа не касаются: заказ, уехавший в сборку, никаких
// денег не закрывает.
func TestOtherStatusesLeavePaymentsAlone(t *testing.T) {
	t.Parallel()

	payments := &cancellingPaymentsStub{}
	response, repository := patchOrder(t, payments, `{"status":"assembling"}`)

	if response.Code != http.StatusOK {
		t.Fatalf("статус ответа %d, ожидали %d", response.Code, http.StatusOK)
	}
	if len(payments.cancelled) != 0 {
		t.Fatalf("сборка заказа тронула платежи: %v", payments.cancelled)
	}
	if repository.statusCalls != 1 {
		t.Fatalf("заказ обновляли %d раз, ожидали один", repository.statusCalls)
	}
}

// Магазин без оплаты картой обязан оставаться рабочим: отменять заказы
// менеджер может и там.
func TestManualCancelWorksWithoutPaymentProvider(t *testing.T) {
	t.Parallel()

	response, repository := patchOrder(t, nil, `{"status":"cancelled"}`)

	if response.Code != http.StatusOK {
		t.Fatalf("статус ответа %d, ожидали %d", response.Code, http.StatusOK)
	}
	if repository.statusCalls != 1 {
		t.Fatalf("заказ обновляли %d раз, ожидали один", repository.statusCalls)
	}
}
