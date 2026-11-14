package payment

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func fiscalQueueExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}

func fiscalQueueMarker(t *testing.T, ctx context.Context, pool *pgxpool.Pool, payment int64, want bool) {
	t.Helper()
	var queued bool
	if err := pool.QueryRow(ctx, `SELECT settlement_requested_at IS NOT NULL FROM payments WHERE id=$1`, payment).Scan(&queued); err != nil || queued != want {
		t.Fatalf("payment %d queued=%t want=%t: %v", payment, queued, want, err)
	}
}

// Reuse a fixture payment's frozen fiscal composition, but make its fulfillment
// scope an individual shipment rather than the parent purchase. All provider
// requests remain in automaticReceiptProvider; no external network is used.
func fiscalQueueOffer(t *testing.T, ctx context.Context, pool *pgxpool.Pool, order, payment int64) int64 {
	t.Helper()
	var offer int64
	if err := pool.QueryRow(ctx, `INSERT INTO shipment_offers(order_id,public_token,order_revision,status,delivery_method,subtotal,total)
	 SELECT $1,$2,1,'paid','pickup',amount,amount FROM payments WHERE id=$3 RETURNING id`, order, fmt.Sprintf("fiscal-queue-offer-%d", payment), payment).Scan(&offer); err != nil {
		t.Fatal(err)
	}
	fiscalQueueExec(t, ctx, pool, `UPDATE payments SET shipment_offer_id=$2,provider_test=false WHERE id=$1`, payment, offer)
	// Remove the restrictive payment -> offer link before the shared fixture's
	// order cleanup cascades through both tables.
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `UPDATE payments SET shipment_offer_id=NULL WHERE shipment_offer_id=$1`, offer); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM shipment_offers WHERE id=$1`, offer); err != nil {
			t.Error(err)
		}
	})
	return offer
}

func fiscalQueueReceipt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, payment int64, want bool) {
	t.Helper()
	var completed bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payment_settlement_receipts WHERE payment_id=$1 AND status='succeeded' AND source='automatic' AND actor_id IS NULL)`, payment).Scan(&completed); err != nil || completed != want {
		t.Fatalf("payment %d automatic receipt=%t want=%t: %v", payment, completed, want, err)
	}
}

func TestIndividualShipmentCompletionQueuesOnlyItsPaymentOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, order, payments := autoFiscalFixture(t)
	offer := fiscalQueueOffer(t, ctx, pool, order, payments[1])
	fiscalQueueExec(t, ctx, pool, `UPDATE shipment_offers SET status='completed' WHERE id=$1`, offer)
	fiscalQueueMarker(t, ctx, pool, payments[0], false)
	fiscalQueueMarker(t, ctx, pool, payments[1], true)
	if err := s.createCompletedSettlementReceipts(ctx); err != nil {
		t.Fatal(err)
	}
	var providerID, parentStatus string
	if err := pool.QueryRow(ctx, `SELECT provider_payment_id FROM payments WHERE id=$1`, payments[1]).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	if len(p.sent) != 1 || p.sent[0].PaymentID != providerID {
		t.Fatalf("shipment completion published another scope: %+v", p.sent)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, order).Scan(&parentStatus); err != nil || parentStatus != "confirmed" {
		t.Fatalf("shipment completion changed parent fulfillment: %s %v", parentStatus, err)
	}
	fiscalQueueReceipt(t, ctx, pool, payments[0], false)
	fiscalQueueReceipt(t, ctx, pool, payments[1], true)
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 1 {
		t.Fatalf("shipment receipt duplicated on next tick: %v", err)
	}
}

func TestParentCompletionWaitsForItsSeparateShipmentOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, order, payments := autoFiscalFixture(t)
	offer := fiscalQueueOffer(t, ctx, pool, order, payments[1])
	fiscalQueueExec(t, ctx, pool, `UPDATE orders SET status='completed' WHERE id=$1`, order)
	fiscalQueueMarker(t, ctx, pool, payments[0], true)
	fiscalQueueMarker(t, ctx, pool, payments[1], false)
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 1 {
		t.Fatalf("parent receipt did not run once: %v", err)
	}
	fiscalQueueReceipt(t, ctx, pool, payments[0], true)
	fiscalQueueReceipt(t, ctx, pool, payments[1], false)
	fiscalQueueExec(t, ctx, pool, `UPDATE shipment_offers SET status='completed' WHERE id=$1`, offer)
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 2 {
		t.Fatalf("later shipment completion did not independently settle: %v", err)
	}
	if p.sent[0].PaymentID == p.sent[1].PaymentID {
		t.Fatal("second fulfillment reposted the parent payment")
	}
	fiscalQueueReceipt(t, ctx, pool, payments[1], true)
}

func TestRolledBackCompletionNeverPublishesReceiptOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, order, payments := autoFiscalFixture(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE orders SET status='completed' WHERE id=$1`, order); err != nil {
		t.Fatal(err)
	}
	var queued bool
	if err = tx.QueryRow(ctx, `SELECT settlement_requested_at IS NOT NULL FROM payments WHERE id=$1`, payments[0]).Scan(&queued); err != nil || !queued {
		t.Fatalf("completion did not durably stage marker in its own transaction: %v", err)
	}
	// A separate worker must not observe the uncommitted completion/marker.
	if err = s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 0 {
		t.Fatalf("worker observed uncommitted completion: %v", err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	for _, payment := range payments {
		fiscalQueueMarker(t, ctx, pool, payment, false)
		fiscalQueueReceipt(t, ctx, pool, payment, false)
	}
	if err = s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 0 {
		t.Fatalf("rollback left a publishable completion: %v", err)
	}
}

func TestRepeatedPaidSyncDoesNotEnqueueHistoricalCompletionOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, order, payments := autoFiscalFixture(t)
	fiscalQueueExec(t, ctx, pool, `UPDATE orders SET status='completed' WHERE id=$1`, order)
	// Reproduce a historical completed purchase, which predates enrollment.
	fiscalQueueExec(t, ctx, pool, `UPDATE payments SET settlement_requested_at=NULL WHERE order_id=$1`, order)
	var providerID string
	if err := pool.QueryRow(ctx, `SELECT provider_payment_id FROM payments WHERE id=$1`, payments[0]).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncOutstanding(ctx, providerID); err != nil {
		t.Fatal(err)
	}
	for _, payment := range payments {
		fiscalQueueMarker(t, ctx, pool, payment, false)
	}
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 0 {
		t.Fatalf("same-status paid sync published historical receipt: %v", err)
	}
	fiscalQueueReceipt(t, ctx, pool, payments[0], false)
}

func TestCompletedOrderSettlesAfterIncomeReceiptConfirmationOnLiveDatabase(t *testing.T) {
	ctx, pool, s, p, order, payments := autoFiscalFixture(t)
	var providerID string
	if err := pool.QueryRow(ctx, `SELECT provider_payment_id FROM payments WHERE id=$1`, payments[0]).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	result := p.payments[providerID]
	result.ReceiptStatus = "pending"
	p.payments[providerID] = result
	fiscalQueueExec(t, ctx, pool, `UPDATE payments SET receipt_status='pending',receipt_checked_at=NULL WHERE id=$1`, payments[0])
	fiscalQueueExec(t, ctx, pool, `UPDATE orders SET status='completed' WHERE id=$1`, order)
	fiscalQueueMarker(t, ctx, pool, payments[0], true)
	if err := s.reconcilePaymentReceipts(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 0 {
		t.Fatalf("unconfirmed income receipt was settled: %v", err)
	}
	result.ReceiptStatus = "succeeded"
	p.payments[providerID] = result
	fiscalQueueExec(t, ctx, pool, `UPDATE payments SET receipt_checked_at=NULL WHERE id=$1`, payments[0])
	if err := s.reconcilePaymentReceipts(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 1 || p.sent[0].PaymentID != providerID {
		t.Fatalf("confirmed income did not release exactly its settlement: %v", err)
	}
	fiscalQueueReceipt(t, ctx, pool, payments[0], true)
	if err := s.createCompletedSettlementReceipts(ctx); err != nil || len(p.sent) != 1 {
		t.Fatalf("confirmed income settlement duplicated: %v", err)
	}
}
