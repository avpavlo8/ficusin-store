package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
)

type adjustmentRepositoryStub struct {
	*adminRepositoryStub
	edits []admin.OrderEdit
}

func (stub *adjustmentRepositoryStub) EditOrder(_ context.Context, _ admin.Actor, id int64, edit admin.OrderEdit) (admin.Order, error) {
	stub.edits = append(stub.edits, edit)
	return admin.Order{ID: id, Total: 3970, PaymentStatus: payment.StatusPending}, nil
}

func (stub *adjustmentRepositoryStub) OrderAdjustment(_ context.Context, id int64) (admin.OrderAdjustmentState, error) {
	return admin.OrderAdjustmentState{
		ID:          id,
		Subtotal:    2970,
		DeliveryFee: 1000,
		Items: []admin.OrderItem{
			{ProductID: 101, ProductName: "Азалия D9", UnitPrice: 790, Quantity: 1},
			{ProductID: 102, ProductName: "Аглаонема Мария", UnitPrice: 1490, Quantity: 1},
		},
	}, nil
}

type adjustmentPaymentsStub struct {
	superseded []int64
}

func (stub *adjustmentPaymentsStub) Refund(context.Context, int64) error { return nil }
func (stub *adjustmentPaymentsStub) SupersedePending(_ context.Context, orderID int64) error {
	stub.superseded = append(stub.superseded, orderID)
	return nil
}
func (stub *adjustmentPaymentsStub) BalanceForOrder(context.Context, int64) (payment.Balance, error) {
	return payment.Balance{Total: 2480, Due: 2480, Ready: true, PaymentStatus: payment.StatusPending}, nil
}
func (stub *adjustmentPaymentsStub) Reconcile(context.Context, int64) (payment.Balance, error) {
	return payment.Balance{Total: 3970, Due: 3970, Ready: false, PaymentStatus: payment.StatusPending}, nil
}
func (stub *adjustmentPaymentsStub) RefundAmount(context.Context, int64, float64, string) (payment.Balance, error) {
	return payment.Balance{}, nil
}
func (stub *adjustmentPaymentsStub) RefundExcess(context.Context, int64, string) (payment.Balance, error) {
	return payment.Balance{}, nil
}
func (stub *adjustmentPaymentsStub) StartOutstandingForOrderID(context.Context, int64) (string, payment.Balance, error) {
	return "", payment.Balance{}, nil
}
func (stub *adjustmentPaymentsStub) RecoverUnknownForOrder(context.Context, int64, int64) (string, error) {
	return "", nil
}
func (stub *adjustmentPaymentsStub) ResolveUnknown(context.Context, int64, int64, string) (payment.Balance, error) {
	return payment.Balance{}, nil
}
func (stub *adjustmentPaymentsStub) DismissUnknown(context.Context, int64, int64) (payment.Balance, error) {
	return payment.Balance{}, nil
}

func TestOrderContentsRetiresOldPendingLinkAndKeepsAddedProduct(t *testing.T) {
	t.Parallel()

	repository := &adjustmentRepositoryStub{adminRepositoryStub: &adminRepositoryStub{}}
	payments := &adjustmentPaymentsStub{}
	dependencies := adminDependencies(repository, admin.RoleOwner, "owner@example.com")
	dependencies.Refunds = payments

	response := httptest.NewRecorder()
	NewRouter(discardLogger(), dependencies).ServeHTTP(
		response,
		adminRequest(http.MethodPatch, "/api/v1/admin/orders/7/contents", `{"items":[{"productId":"azaliya-d9","quantity":1},{"productId":"aglaonema-mariya","quantity":1}]}`),
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if len(payments.superseded) != 1 || payments.superseded[0] != 7 {
		t.Fatalf("old pending link was not retired before edit: %v", payments.superseded)
	}
	if len(repository.edits) != 1 || repository.edits[0].Items == nil {
		t.Fatalf("edit did not reach repository: %+v", repository.edits)
	}
	items := *repository.edits[0].Items
	if len(items) != 2 || items[1].ProductID != "aglaonema-mariya" {
		t.Fatalf("added product disappeared before persistence: %+v", items)
	}
}

func TestMoneyActionsRequireOwnerPermission(t *testing.T) {
	for _, role := range []string{admin.RoleManager, admin.RoleAdministrator} {
		for _, tc := range []struct{ method, path, body string }{
			{"POST", "/api/v1/admin/orders/7/refund", `{"amount":10,"reason":"fixture"}`},
			{"PATCH", "/api/v1/admin/orders/7", `{"refund":true}`},
			{"POST", "/api/v1/admin/orders/7/payments/1/settlement-receipt", `{"confirmed":true}`},
			{"POST", "/api/v1/admin/orders/7/refunds/1/resolve", `{"providerId":"ref-1"}`},
			{"POST", "/api/v1/admin/orders/7/payments/1/dismiss", `{"confirmed":true}`},
		} {
			deps := adminDependencies(&adminRepositoryStub{}, role, "")
			deps.Refunds = &adjustmentPaymentsStub{}
			res := httptest.NewRecorder()
			NewRouter(discardLogger(), deps).ServeHTTP(res, adminRequest(tc.method, tc.path, tc.body))
			if res.Code != http.StatusForbidden {
				t.Fatalf("%s %s: status=%d body=%s", role, tc.path, res.Code, res.Body.String())
			}
		}
	}
}

type settlementPaymentsStub struct {
	adjustmentPaymentsStub
	calls int
}

func (s *settlementPaymentsStub) SettlePaymentReceipt(context.Context, int64, int64, int64) error {
	s.calls++
	return nil
}
func TestSettlementReceiptRequiresExplicitConfirmation(t *testing.T) {
	stub := &settlementPaymentsStub{}
	deps := adminDependencies(&adminRepositoryStub{}, admin.RoleOwner, "")
	deps.Refunds = stub
	router := NewRouter(discardLogger(), deps)
	for _, body := range []string{`{}`, `{"confirmed":false}`} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, adminRequest("POST", "/api/v1/admin/orders/7/payments/1/settlement-receipt", body))
		if response.Code != 400 || stub.calls != 0 {
			t.Fatalf("unconfirmed fiscal action: %d calls=%d", response.Code, stub.calls)
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, adminRequest("POST", "/api/v1/admin/orders/7/payments/1/settlement-receipt", `{"confirmed":true}`))
	if response.Code != 200 || stub.calls != 1 {
		t.Fatalf("confirmed request: %d calls=%d %s", response.Code, stub.calls, response.Body.String())
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, adminRequest("POST", "/api/v1/admin/orders/7/refunds/1/receipt", `{"confirmed":true}`))
	if response.Code != 404 {
		t.Fatal("obsolete own-till route still registered")
	}
}
