package operations

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercise the actual aggregate query and real payment/refund joins. Every
// fixture lives in one rolled-back transaction on an explicitly isolated DB.
func TestFiscalHealthClassifiesTestPaymentsAndSettlementGraceOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var database string
	if err = pool.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	if database != "ficusin_local_publisher_test" && database != "ficusin_auto_fiscal_test" && database != "ficusin_ci" {
		t.Skip("requires isolated fiscal/publisher fixture database")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	exec := func(sql string, args ...any) { t.Helper(); _, e := tx.Exec(ctx, sql, args...); must(e) }
	counts := func() map[string]int64 {
		t.Helper()
		rows, e := tx.Query(ctx, snapshotSQL)
		must(e)
		defer rows.Close()
		result := map[string]int64{}
		for rows.Next() {
			var check Check
			must(rows.Scan(&check.Code, &check.Severity, &check.Affected, &check.OldestSeconds))
			result[check.Code] = check.Affected
		}
		must(rows.Err())
		return result
	}
	baseline := counts()
	tag, sequence := fmt.Sprint(time.Now().UnixNano()), 0
	seed := func(test any, mode string) (int64, int64) {
		t.Helper()
		sequence++
		key := fmt.Sprintf("fiscal-health-%s-%d", tag, sequence)
		var order, payment int64
		must(tx.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status)
		 VALUES($1,'fixture','+70000000000','fixture@example.invalid','pickup',0,100,100,'online','pending','completed') RETURNING id`, key).Scan(&order))
		payload := fmt.Sprintf(`{"ReceiptEnabled":true,"Items":[{"Name":"fixture","Price":100,"Quantity":1,"Mode":%q}]}`, mode)
		must(tx.QueryRow(ctx, `INSERT INTO payments(order_id,provider_payment_id,idempotence_key,amount,status,receipt_status,provider_test,request_payload,created_at)
		 VALUES($1,$2,$2,100,'paid','unknown',$3,$4::jsonb,CURRENT_TIMESTAMP-INTERVAL '4 days') RETURNING id`, order, key, test, payload).Scan(&payment))
		// Simulate a historical completion, before durable automatic enrollment.
		exec(`UPDATE payments SET settlement_requested_at=NULL WHERE id=$1`, payment)
		return order, payment
	}
	remove := func(order, payment int64) {
		t.Helper()
		exec(`DELETE FROM payment_settlement_receipts WHERE payment_id=$1`, payment)
		exec(`DELETE FROM payment_refunds WHERE payment_id=$1`, payment)
		exec(`DELETE FROM orders WHERE id=$1`, order)
	}
	checks := []string{"provider_paid_order_unsettled", "payment_receipt_required", "payment_settlement_receipt_required", "refund_manual_review", "refund_receipt_required"}
	for _, classification := range []struct {
		name string
		test any
		want int64
	}{{"confirmed test", true, 0}, {"confirmed live", false, 1}, {"unverified", nil, 1}} {
		t.Run(classification.name, func(t *testing.T) {
			order, payment := seed(classification.test, "full_prepayment")
			for i, status := range []string{"pending", "succeeded"} {
				exec(`INSERT INTO payment_refunds(order_id,payment_id,idempotence_key,amount,status,needs_review,receipt_status,created_at)
				 VALUES($1,$2,$3,1,$4,$5,'unknown',CURRENT_TIMESTAMP-INTERVAL '4 days')`, order, payment, fmt.Sprintf("%s-refund-%d-%d", tag, sequence, i), status, status == "pending")
			}
			actual := counts()
			for _, code := range checks {
				if actual[code]-baseline[code] != classification.want {
					t.Fatalf("%s delta=%d, want %d", code, actual[code]-baseline[code], classification.want)
				}
			}
			remove(order, payment)
		})
	}
	for _, classification := range []struct {
		name string
		test any
		want int64
	}{{"test", true, 0}, {"live", false, 1}, {"unknown", nil, 1}} {
		t.Run("stale pending "+classification.name, func(t *testing.T) {
			order, payment := seed(classification.test, "full_prepayment")
			exec(`UPDATE orders SET status='new' WHERE id=$1`, order)
			exec(`UPDATE payments SET status='pending' WHERE id=$1`, payment)
			actual := counts()
			if actual["stale_pending_payment"]-baseline["stale_pending_payment"] != classification.want {
				t.Fatal("stale pending classification lost tri-state test semantics")
			}
			remove(order, payment)
		})
	}
	for _, scenario := range []struct {
		name, mode, requested, receipt string
		want                           int64
	}{
		{"legacy missing prepayment receipt", "full_prepayment", "NULL", "", 1},
		{"new request grace", "full_prepayment", "CURRENT_TIMESTAMP", "", 0},
		{"overdue request", "full_prepayment", "CURRENT_TIMESTAMP-INTERVAL '6 minutes'", "", 1},
		{"legacy unknown mode", "", "NULL", "", 1},
		{"full payment needs no settlement", "full_payment", "NULL", "", 0},
		{"failed during grace", "full_prepayment", "CURRENT_TIMESTAMP", "failed", 1},
		{"manual review during grace", "full_prepayment", "CURRENT_TIMESTAMP", "manual_review", 1},
		{"canceled during grace", "full_prepayment", "CURRENT_TIMESTAMP", "canceled", 1},
		{"old pending", "full_prepayment", "CURRENT_TIMESTAMP", "pending", 1},
		{"succeeded", "full_prepayment", "NULL", "succeeded", 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			order, payment := seed(false, scenario.mode)
			// requested is selected solely from the fixed fixture table above.
			exec(`UPDATE payments SET settlement_requested_at=`+scenario.requested+` WHERE id=$1`, payment)
			if scenario.receipt != "" {
				exec(`INSERT INTO payment_settlement_receipts(payment_id,idempotence_key,request_payload,status,source,created_at)
				 VALUES($1,$2,'{}',$3,'automatic',CURRENT_TIMESTAMP-INTERVAL '4 days')`, payment, fmt.Sprintf("%s-settlement-%d", tag, sequence), scenario.receipt)
			}
			actual := counts()
			if actual["payment_settlement_receipt_required"]-baseline["payment_settlement_receipt_required"] != scenario.want {
				t.Fatalf("settlement warning delta=%d want %d", actual["payment_settlement_receipt_required"]-baseline["payment_settlement_receipt_required"], scenario.want)
			}
			remove(order, payment)
		})
	}
	for name, payload := range map[string]string{
		"items object": `{"ReceiptEnabled":true,"Items":{}}`,
		"items null":   `{"ReceiptEnabled":true,"Items":null}`,
		"items empty":  `{"ReceiptEnabled":true,"Items":[]}`,
	} {
		t.Run("malformed snapshot "+name, func(t *testing.T) {
			order, payment := seed(false, "full_prepayment")
			exec(`UPDATE payments SET request_payload=$2::jsonb WHERE id=$1`, payment, payload)
			var mode string
			must(tx.QueryRow(ctx, `SELECT payment_fiscal_mode(request_payload) FROM payments WHERE id=$1`, payment).Scan(&mode))
			if mode != "unknown" {
				t.Fatalf("malformed snapshot classified as %q", mode)
			}
			actual := counts()
			if actual["payment_settlement_receipt_required"]-baseline["payment_settlement_receipt_required"] != 1 {
				t.Fatal("malformed historical snapshot was hidden from settlement review")
			}
			remove(order, payment)
		})
	}
}
