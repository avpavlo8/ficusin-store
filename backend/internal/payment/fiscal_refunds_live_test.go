package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

type fiscalRefundProvider struct {
	livePaymentProvider
	requests []integration.RefundRequest
	results  map[string]integration.Refund
	getError bool
}

func (p *fiscalRefundProvider) PrepareRefund(id string, amount float64, key string, original integration.PaymentRequest) (integration.RefundRequest, error) {
	return integration.NewYooKassaClient("fixture", "fixture", 0, 1).PrepareRefund(id, amount, key, original)
}
func (p *fiscalRefundProvider) CreateRefund(_ context.Context, r integration.RefundRequest) (integration.Refund, error) {
	p.requests = append(p.requests, r)
	result := integration.Refund{ID: "fixture-" + r.IdempotenceKey, PaymentID: r.PaymentID, Amount: r.Amount, Status: "succeeded", ReceiptStatus: "pending"}
	p.results[result.ID] = result
	return result, nil
}
func (p *fiscalRefundProvider) FetchRefund(_ context.Context, id string) (integration.Refund, error) {
	if p.getError {
		return integration.Refund{}, errors.New("fixture GET unavailable")
	}
	return p.results[id], nil
}
func fiscalFixture(t *testing.T) (context.Context, *pgxpool.Pool, *Service, *fiscalRefundProvider, int64, []int64) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	number := fmt.Sprintf("FISCAL-FIXTURE-%d", time.Now().UnixNano())
	var orderID int64
	if err = pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status) VALUES($1,'fixture','+70000000000','fixture@example.invalid','pickup',0,277.79,277.79,'online','paid','confirmed') RETURNING id`, number).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM payment_settlement_receipts WHERE payment_id IN(SELECT id FROM payments WHERE order_id=$1)`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM payment_refunds WHERE order_id=$1`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, orderID)
		pool.Close()
	})
	ids := []int64{}
	for i, items := range [][]integration.PaymentItem{{{Name: "Растение", Price: 100.01, Quantity: 2}}, {{Name: "Доставка", Price: 77.77, Quantity: 1, Subject: "service"}}} {
		original := integration.NewYooKassaClient("fixture", "fixture", 2, 6).SnapshotReceipt(integration.PaymentRequest{Email: "fixture@example.invalid", Items: items})
		raw, _ := json.Marshal(original)
		var id int64
		amount := 200.02
		if i == 1 {
			amount = 77.77
		}
		if err = pool.QueryRow(ctx, `INSERT INTO payments(order_id,idempotence_key,provider_payment_id,amount,status,request_payload) VALUES($1,$2,$2,$3,'paid',$4) RETURNING id`, orderID, fmt.Sprintf("%s-%d", number, i), amount, raw).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	provider := &fiscalRefundProvider{results: map[string]integration.Refund{}}
	service := NewService(pool, provider, "https://fixture.invalid", slog.New(slog.NewTextHandler(io.Discard, nil)))
	return ctx, pool, service, provider, orderID, ids
}
func TestExactPartialFiscalRefundsOnDatabaseOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, orderID, ids := fiscalFixture(t)
	selected := []RefundSelection{{ids[0], 0, 1}, {ids[1], 0, 1}}
	request := WithRefundSelections(WithRefundRequest(ctx, "fixture-refund-request-1"), selected)
	if _, err := s.RefundAmount(request, orderID, 177.77, "fixture"); err == nil {
		t.Fatal("one kopeck mismatch accepted")
	}
	var count int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM payment_refunds WHERE order_id=$1`, orderID).Scan(&count)
	if count != 0 || len(p.requests) != 0 {
		t.Fatal("invalid selection reached provider")
	}
	balance, err := s.RefundAmount(request, orderID, 177.78, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if balance.Refunded != 177.78 || len(p.requests) != 2 {
		t.Fatalf("split missing: %+v", balance)
	}
	for _, req := range p.requests {
		raw, _ := json.Marshal(req.Receipt)
		if !strings.Contains(string(raw), `"vat_code":6`) || !strings.Contains(string(raw), `"tax_system_code":2`) {
			t.Fatalf("fiscal snapshot lost: %s", raw)
		}
	}
	if _, err = s.RefundAmount(WithRefundSelections(WithRefundRequest(ctx, "fixture-refund-request-1"), []RefundSelection{selected[1], selected[0]}), orderID, 177.78, "fixture"); err != nil || len(p.requests) != 2 {
		t.Fatalf("retry: %v requests=%d", err, len(p.requests))
	}
	if _, err = s.RefundAmount(WithRefundSelections(WithRefundRequest(ctx, "fixture-refund-request-1"), []RefundSelection{{ids[0], 0, 1}}), orderID, 177.78, "fixture"); err == nil {
		t.Fatal("same key accepted different positions")
	}
	if _, err = s.RefundAmount(WithRefundSelections(ctx, []RefundSelection{{ids[0], 0, 2}}), orderID, 200.02, "fixture"); err == nil {
		t.Fatal("returned units available again")
	}
	options, err := s.refundOptions(ctx, orderID)
	if err != nil || len(options) != 1 || options[0].Quantity != 1 || options[0].Price != 100.01 {
		t.Fatalf("options %+v %v", options, err)
	}
	p.getError = true
	if err = s.syncRefund(ctx, balance.Refunds[0].ID); err == nil {
		t.Fatal("fixture GET failure missing")
	}
	after, err := s.BalanceForOrder(ctx, orderID)
	if err != nil || after.Refunded != 177.78 || after.Refunds[0].Status != "succeeded" {
		t.Fatalf("money status changed on GET error: %+v %v", after, err)
	}
	p.getError = false
	for id, result := range p.results {
		result.ReceiptStatus = "succeeded"
		p.results[id] = result
	}
	if err = s.syncRefund(ctx, balance.Refunds[0].ID); err != nil {
		t.Fatal(err)
	}
	after, err = s.BalanceForOrder(ctx, orderID)
	if err != nil || after.Refunds[0].ReceiptStatus != "succeeded" {
		t.Fatalf("fiscal status not refreshed %+v %v", after, err)
	}
}
func TestSelectionRejectsDuplicatesAndForeignLinesOnLiveDatabase(t *testing.T) {
	ctx, _, s, p, orderID, ids := fiscalFixture(t)
	for _, items := range [][]RefundSelection{{{ids[0], 0, 1}, {ids[0], 0, 1}}, {{ids[0], -1, 1}}, {{ids[0], 0, 0}}, {{ids[0] + 999999, 0, 1}}, {{ids[0], 2, 1}}} {
		if _, err := s.RefundAmount(WithRefundSelections(ctx, items), orderID, 100.01, "fixture"); err == nil {
			t.Fatalf("invalid %+v accepted", items)
		}
	}
	if len(p.requests) != 0 {
		t.Fatal("invalid positions posted")
	}
}

type fixtureReceiptReader struct {
	*fiscalRefundProvider
	payments map[string]integration.Payment
	reads    int
}

func (p *fixtureReceiptReader) FetchPayment(_ context.Context, id string) (integration.Payment, error) {
	p.reads++
	return p.payments[id], nil
}
func TestPaymentReceiptPollingDoesNotMoveMoneyOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, orderID, ids := fiscalFixture(t)
	reader := &fixtureReceiptReader{fiscalRefundProvider: p, payments: map[string]integration.Payment{}}
	for i, id := range ids {
		var providerID string
		if err := pool.QueryRow(ctx, `SELECT provider_payment_id FROM payments WHERE id=$1`, id).Scan(&providerID); err != nil {
			t.Fatal(err)
		}
		amount := 200.02
		if i == 1 {
			amount = 77.77
		}
		reader.payments[providerID] = integration.Payment{ID: providerID, Status: "succeeded", Amount: amount, ReceiptStatus: "succeeded"}
	}
	s.provider = reader
	if err := s.reconcilePaymentReceipts(ctx); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.fiscalTasks(ctx, orderID)
	if err != nil || len(tasks) != 2 || tasks[0].Status != "succeeded" || tasks[1].Status != "succeeded" {
		t.Fatalf("receipt polling failed: %+v %v", tasks, err)
	}
	if reader.reads < 2 || len(p.requests) != 0 || p.creates != 0 {
		t.Fatal("receipt polling moved money or skipped reads")
	}

}
