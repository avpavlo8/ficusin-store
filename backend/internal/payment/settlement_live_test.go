package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"strings"
	"testing"
	"time"
)

type fixtureSettlementProvider struct {
	*fiscalRefundProvider
	sent    []integration.SettlementReceiptRequest
	result  integration.FiscalReceipt
	unknown bool
}

func (p *fixtureSettlementProvider) PrepareSettlementReceipt(id, key string, original integration.PaymentRequest, items []integration.PaymentItem) (integration.SettlementReceiptRequest, error) {
	return integration.NewYooKassaClient("fixture", "fixture", 0, 1).PrepareSettlementReceipt(id, key, original, items)
}
func (p *fixtureSettlementProvider) CreateSettlementReceipt(_ context.Context, r integration.SettlementReceiptRequest) (integration.FiscalReceipt, error) {
	p.sent = append(p.sent, r)
	p.result = integration.FiscalReceipt{ID: "fixture-receipt", PaymentID: r.PaymentID, Type: "payment", Status: "pending"}
	if p.unknown {
		return integration.FiscalReceipt{}, integration.ErrPaymentOutcomeUnknown
	}
	return p.result, nil
}
func (p *fixtureSettlementProvider) FetchReceipt(context.Context, string) (integration.FiscalReceipt, error) {
	return p.result, nil
}
func TestSettlementReceiptRequiresOwnerActionAndSurvivesTimeoutOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, orderID, ids := fiscalFixture(t)
	provider := &fixtureSettlementProvider{fiscalRefundProvider: p, unknown: true}
	s.provider = provider
	var actor int64
	if err := pool.QueryRow(ctx, `INSERT INTO customers(phone,full_name,consent_at)VALUES($1,'fixture',CURRENT_TIMESTAMP)RETURNING id`, fmt.Sprintf("settlement-fixture-%d", time.Now().UnixNano())).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM payment_settlement_receipts WHERE payment_id=ANY($1::bigint[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actor)
	})
	if err := s.SettlePaymentReceipt(ctx, orderID, ids[0], actor); err == nil {
		t.Fatal("unissued goods accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='completed' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	if err := s.SettlePaymentReceipt(ctx, orderID, ids[0], actor); err == nil {
		t.Fatal("unregistered income receipt accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE payments SET receipt_status='succeeded' WHERE order_id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	if err := s.SettlePaymentReceipt(ctx, orderID, ids[0], actor); err == nil {
		t.Fatal("unknown result disappeared")
	}
	if len(provider.sent) != 1 {
		t.Fatal("no first receipt request")
	}
	if err := s.reconcileSettlementReceipts(ctx); err != nil || len(provider.sent) != 1 {
		t.Fatal("worker created a receipt without owner action")
	}
	provider.unknown = false
	if err := s.SettlePaymentReceipt(ctx, orderID, ids[0], actor); err != nil {
		t.Fatal(err)
	}
	if len(provider.sent) != 2 || provider.sent[0].Key != provider.sent[1].Key {
		t.Fatal("receipt retry changed key")
	}
	provider.result.Status = "succeeded"
	if err := s.SettlePaymentReceipt(ctx, orderID, ids[0], actor); err != nil || len(provider.sent) != 2 {
		t.Fatalf("known receipt was posted again: %v", err)
	}
	var status string
	var savedActor int64
	if err := pool.QueryRow(ctx, `SELECT status,actor_id FROM payment_settlement_receipts WHERE payment_id=$1`, ids[0]).Scan(&status, &savedActor); err != nil || status != "succeeded" || savedActor != actor {
		t.Fatalf("fiscal fact/audit not saved %s %d %v", status, savedActor, err)
	}
	tasks, err := s.fiscalTasks(ctx, orderID)
	if err != nil || tasks[1].Kind != "settlement" || tasks[1].Status != "succeeded" {
		t.Fatalf("tasks %+v %v", tasks, err)
	}
	// A later partial refund uses the final calculation mode, not prepayment.
	refundCtx := WithRefundSelections(ctx, []RefundSelection{{ids[0], 0, 1}})
	if _, err = s.RefundAmount(refundCtx, orderID, 100.01, "fixture after issue"); err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 1 {
		t.Fatal("partial refund after settlement missing")
	}
	encoded, _ := json.Marshal(p.requests[0].Receipt)
	if !strings.Contains(string(encoded), `"payment_mode":"full_payment"`) {
		t.Fatalf("refund used wrong fiscal mode: %s", encoded)
	}

}
