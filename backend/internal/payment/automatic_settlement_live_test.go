package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
)

// No network: the provider keeps its own receipt history, including a receipt
// accepted before a lost response. Repeating a key returns the original fact.
type automaticReceiptProvider struct {
	*fiscalRefundProvider
	mu           sync.Mutex
	payments     map[string]integration.Payment
	receipts     map[string]integration.FiscalReceipt
	sent         []integration.SettlementReceiptRequest
	reads        int
	paymentReads int
	unknown      bool
	getError     bool
	onSend       func(integration.SettlementReceiptRequest)
}

func (p *automaticReceiptProvider) FetchPayment(_ context.Context, id string) (integration.Payment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paymentReads++
	if p.getError {
		return integration.Payment{}, errors.New("fixture not found")
	}
	return p.payments[id], nil
}
func (p *automaticReceiptProvider) PrepareSettlementReceipt(id, key string, original integration.PaymentRequest, items []integration.PaymentItem) (integration.SettlementReceiptRequest, error) {
	return integration.NewYooKassaClient("fixture", "fixture", 0, 1).PrepareSettlementReceipt(id, key, original, items)
}
func (p *automaticReceiptProvider) CreateSettlementReceipt(_ context.Context, r integration.SettlementReceiptRequest) (integration.FiscalReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.onSend != nil {
		p.onSend(r)
	}
	p.sent = append(p.sent, r)
	id := "receipt-" + r.Key
	if _, exists := p.receipts[id]; !exists {
		p.receipts[id] = integration.FiscalReceipt{ID: id, PaymentID: r.PaymentID, Type: "payment", Status: "succeeded"}
	}
	if p.unknown {
		return integration.FiscalReceipt{}, integration.ErrPaymentOutcomeUnknown
	}
	return p.receipts[id], nil
}
func (p *automaticReceiptProvider) FetchReceipt(_ context.Context, id string) (integration.FiscalReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads++
	return p.receipts[id], nil
}

func autoFiscalFixture(t *testing.T) (context.Context, *pgxpool.Pool, *Service, *automaticReceiptProvider, int64, []int64) {
	t.Helper()
	ctx, pool, s, original, orderID, ids := fiscalFixture(t)
	p := &automaticReceiptProvider{fiscalRefundProvider: original, payments: map[string]integration.Payment{}, receipts: map[string]integration.FiscalReceipt{}}
	for i, id := range ids {
		var providerID string
		var amount float64
		if err := pool.QueryRow(ctx, `SELECT provider_payment_id,amount::double precision FROM payments WHERE id=$1`, id).Scan(&providerID, &amount); err != nil {
			t.Fatal(err)
		}
		live := false
		p.payments[providerID] = integration.Payment{ID: providerID, Status: "succeeded", Paid: true, Amount: amount, ReceiptStatus: "succeeded", Test: &live}
		if _, err := pool.Exec(ctx, `UPDATE payments SET receipt_status='succeeded',provider_test=$2 WHERE id=$1`, id, i != 0); err != nil {
			t.Fatal(err)
		}
	}
	s.provider = p
	return ctx, pool, s, p, orderID, ids
}

func TestAutomaticSettlementIsDurableAndConcurrentOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, orderID, ids := autoFiscalFixture(t)
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 0 {
		t.Fatalf("unissued order posted: %v", err)
	}
	p.onSend = func(r integration.SettlementReceiptRequest) {
		var source, key string
		var actor *int64
		var raw []byte
		if err := pool.QueryRow(ctx, `SELECT source,actor_id,idempotence_key,request_payload FROM payment_settlement_receipts WHERE payment_id=$1`, ids[0]).Scan(&source, &actor, &key, &raw); err != nil {
			t.Error(err)
		}
		var saved integration.SettlementReceiptRequest
		if err := json.Unmarshal(raw, &saved); err != nil || source != "automatic" || actor != nil || key != r.Key || !reflect.DeepEqual(saved.Body, r.Body) {
			t.Error("automatic audit and exact request must be durable before POST")
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='completed' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if err := s.createCompletedSettlementReceipts(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if len(p.sent) != 1 {
		t.Fatalf("concurrent workers posted %d times", len(p.sent))
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='completed' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 1 {
		t.Fatalf("repeat completion duplicated receipt: %v", err)
	}
	tasks, err := s.fiscalTasks(ctx, orderID)
	if err != nil || len(tasks) != 2 || tasks[1].Status != "succeeded" {
		t.Fatalf("fiscal facts/test filtering: %+v %v", tasks, err)
	}
}

func TestAutomaticSettlementRecoversLostResponseOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, orderID, ids := autoFiscalFixture(t)
	p.unknown = true
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='completed' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 1 {
		t.Fatalf("first attempt %v", err)
	}
	// Simulate a process restart and a later worker tick.
	s = NewService(pool, p, "https://fixture.invalid", s.logger)
	p.unknown = false
	if _, err := pool.Exec(ctx, `UPDATE payment_settlement_receipts SET updated_at=CURRENT_TIMESTAMP-INTERVAL '2 minutes' WHERE payment_id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcileSettlementReceipts(ctx); err != nil || len(p.sent) != 2 {
		t.Fatalf("retry missing: %v", err)
	}
	if p.sent[0].Key != p.sent[1].Key || !reflect.DeepEqual(p.sent[0].Body, p.sent[1].Body) || len(p.receipts) != 1 {
		t.Fatal("lost response created a second fiscal receipt")
	}
	// A known ID must be read with GET even after the idempotence window.
	if _, err := pool.Exec(ctx, `UPDATE payment_settlement_receipts SET status='pending',created_at=CURRENT_TIMESTAMP-INTERVAL '2 days',updated_at=CURRENT_TIMESTAMP-INTERVAL '2 minutes' WHERE payment_id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcileSettlementReceipts(ctx); err != nil || len(p.sent) != 2 || p.reads != 1 {
		t.Fatalf("known receipt was reposted: %v", err)
	}
}

func TestAutomaticSettlementStopsExpiredUnknownAttemptOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, orderID, ids := autoFiscalFixture(t)
	p.unknown = true
	_, _ = pool.Exec(ctx, `UPDATE orders SET status='completed' WHERE id=$1`, orderID)
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 1 {
		t.Fatal(err)
	}
	_, _ = pool.Exec(ctx, `UPDATE payment_settlement_receipts SET created_at=CURRENT_TIMESTAMP-INTERVAL '24 hours',updated_at=CURRENT_TIMESTAMP-INTERVAL '2 minutes' WHERE payment_id=$1`, ids[0])
	if err := s.reconcileSettlementReceipts(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM payment_settlement_receipts WHERE payment_id=$1`, ids[0]).Scan(&status); err != nil || status != "manual_review" || len(p.sent) != 1 {
		t.Fatalf("expired unknown attempt was reposted: %s %v", status, err)
	}
}

func TestAutomaticSettlementRejectsUnsafeCasesOnLiveDatabase(t *testing.T) {
	for _, name := range []string{"test", "unknown_test", "wrong_id", "wrong_amount", "lookup_error", "income_pending", "unpaid", "full_payment", "mixed_mode", "pending_refund", "full_refund", "legacy_amount_refund", "legacy_completion"} {
		t.Run(name, func(t *testing.T) {
			ctx, pool, s, p, orderID, ids := autoFiscalFixture(t)
			_, err := pool.Exec(ctx, `UPDATE orders SET status='completed' WHERE id=$1`, orderID)
			if err != nil {
				t.Fatal(err)
			}
			for id, result := range p.payments {
				switch name {
				case "test":
					value := true
					result.Test = &value
				case "unknown_test":
					result.Test = nil
				case "wrong_id":
					result.ID = "another-payment"
				case "wrong_amount":
					result.Amount++
				case "income_pending":
					result.ReceiptStatus = "pending"
				case "unpaid":
					result.Paid = false
				}
				p.payments[id] = result
			}
			var sql string
			switch name {
			case "lookup_error":
				p.getError = true
			case "full_payment":
				sql = `UPDATE payments SET request_payload=jsonb_set(request_payload,'{Items,0,Mode}','"full_payment"') WHERE id=$1`
			case "mixed_mode":
				sql = `UPDATE payments SET request_payload=jsonb_set(request_payload,'{Items}',(request_payload->'Items')||'[ {"Name":"other","Mode":"full_payment","Quantity":1,"Price":1}]') WHERE id=$1`
			case "legacy_completion":
				// Existing completed records were not enqueued at migration time.
				// Replayed same-status provider updates must not enqueue them either.
				sql = `UPDATE payments SET settlement_requested_at=NULL,status='paid' WHERE id=$1`
			case "pending_refund", "full_refund", "legacy_amount_refund":
				status, amount := "pending", 100.01
				if name != "pending_refund" {
					status = "succeeded"
				}
				if name == "full_refund" {
					amount = 200.02
				}
				_, err = pool.Exec(ctx, `INSERT INTO payment_refunds(order_id,payment_id,idempotence_key,amount,reason,status)VALUES($1,$2,$3,$4,'fixture',$5)`, orderID, ids[0], fmt.Sprintf("auto-refund-%d", ids[0]), amount, status)
			}
			if sql != "" {
				_, err = pool.Exec(ctx, sql, ids[0])
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 0 {
				t.Fatalf("unsafe receipt posted: %d %v", len(p.sent), err)
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM payment_settlement_receipts WHERE payment_id=$1`, ids[0]).Scan(&count); err != nil || count != 0 {
				t.Fatalf("unsafe attempt persisted: %d %v", count, err)
			}
			if name == "test" {
				var test *bool
				if err := pool.QueryRow(ctx, `SELECT provider_test FROM payments WHERE id=$1`, ids[0]).Scan(&test); err != nil || test == nil || !*test {
					t.Fatal("verified test fact not saved")
				}
			}
		})
	}
}

func TestAutomaticSettlementUsesRemainingFiscalUnitsOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, orderID, ids := autoFiscalFixture(t)
	selection, _ := json.Marshal([]RefundSelection{{PaymentID: ids[0], ItemIndex: 0, Quantity: 1}})
	if _, err := pool.Exec(ctx, `INSERT INTO payment_refunds(order_id,payment_id,idempotence_key,amount,reason,status,selections)VALUES($1,$2,$3,100.01,'fixture','succeeded',$4)`, orderID, ids[0], fmt.Sprintf("auto-partial-%d", ids[0]), selection); err != nil {
		t.Fatal(err)
	}
	_, _ = pool.Exec(ctx, `UPDATE orders SET status='completed' WHERE id=$1`, orderID)
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 1 {
		t.Fatalf("remaining items not settled: %v", err)
	}
	raw, _ := json.Marshal(p.sent[0].Body)
	var body struct {
		Items []struct {
			Quantity string `json:"quantity"`
		}
		Settlements []struct {
			Amount struct {
				Value string `json:"value"`
			} `json:"amount"`
		}
	}
	if err := json.Unmarshal(raw, &body); err != nil || len(body.Items) != 1 || body.Items[0].Quantity != "1" || body.Settlements[0].Amount.Value != "100.01" {
		t.Fatalf("settled returned units: %s %v", raw, err)
	}
}

func TestAutomaticSettlementHandlesSplitRealPaymentsOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, orderID, _ := autoFiscalFixture(t)
	if _, err := pool.Exec(ctx, `UPDATE payments SET provider_test=false WHERE order_id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='completed' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 2 || len(p.receipts) != 2 || p.sent[0].Key == p.sent[1].Key {
		t.Fatalf("split payments were not settled separately: %+v %v", p.sent, err)
	}
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 2 {
		t.Fatalf("split payment duplicated: %v", err)
	}
}

func TestPaymentIdentityMismatchCannotCaptureOnLiveDatabase(t *testing.T) {
	ctx, _, s, p, _, _ := autoFiscalFixture(t)
	for id, result := range p.payments {
		result.ID = "another-payment"
		result.Status = "waiting_for_capture"
		p.payments[id] = result
		if err := s.SyncOutstanding(ctx, id); err == nil {
			t.Fatal("mismatched provider ID accepted")
		}
	}
	if p.captures != 0 || p.cancels != 0 {
		t.Fatal("mismatched lookup reached a financial POST")
	}
}

func TestReceiptLookupFailurePreservesFactsAndBackoffOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, _, ids := autoFiscalFixture(t)
	p.getError = true
	if _, err := pool.Exec(ctx, `UPDATE payments SET provider_test=NULL,receipt_status='unknown',receipt_checked_at=NULL WHERE id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.reconcilePaymentReceipts(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var test *bool
	var status, receipt string
	var checked bool
	if err := pool.QueryRow(ctx, `SELECT provider_test,status,receipt_status,receipt_checked_at IS NOT NULL FROM payments WHERE id=$1`, ids[0]).Scan(&test, &status, &receipt, &checked); err != nil {
		t.Fatal(err)
	}
	if test != nil || status != "paid" || receipt != "unknown" || !checked || p.paymentReads != 1 || len(p.sent) != 0 {
		t.Fatal("failed GET changed fiscal/money facts or bypassed backoff")
	}
}
