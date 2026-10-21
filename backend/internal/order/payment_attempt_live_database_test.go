package order

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAccountPaymentAttemptOnLiveDatabase(t *testing.T) {
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
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	number := "CI-ATTEMPT-" + suffix
	var customerID, orderID, pendingID int64
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','CI payment attempt',CURRENT_TIMESTAMP) RETURNING id`, "attempt-"+suffix+"@example.invalid", "+799900"+suffix[len(suffix)-5:]).Scan(&customerID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customerID)
	}()
	if err := pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status) VALUES($1,$2,'CI payment attempt','+79990000001','attempt@example.invalid','pickup',0,1490,1490,'online','pending','confirmed') RETURNING id`, number, customerID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(pool)
	check := func(want string) *Detail {
		t.Helper()
		detail, err := repository.DetailForCustomer(ctx, customerID, number)
		if err != nil {
			t.Fatal(err)
		}
		if detail == nil || detail.PaymentAttemptStatus != want {
			t.Fatalf("attempt: got %#v; want %q", detail, want)
		}
		return detail
	}
	check("")
	if _, err := pool.Exec(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status) VALUES($1,$2,1490,'canceled')`, orderID, suffix+"-cancelled"); err != nil {
		t.Fatal(err)
	}
	if detail := check("canceled"); detail.PaymentStatus != "pending" || detail.AmountDue != 1490 {
		t.Fatalf("attempt changed order balance: %#v", detail)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status) VALUES($1,$2,1490,'pending') RETURNING id`, orderID, suffix+"-retry").Scan(&pendingID); err != nil {
		t.Fatal(err)
	}
	check("pending")
	// An active attempt takes precedence even if another cancellation arrives.
	if _, err := pool.Exec(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status) VALUES($1,$2,1490,'canceled')`, orderID, suffix+"-late-cancelled"); err != nil {
		t.Fatal(err)
	}
	check("pending")
	if _, err := pool.Exec(ctx, `UPDATE payments SET status='paid' WHERE id=$1`, pendingID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET payment_status='paid' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	if detail := check("canceled"); detail.PaidAmount != 1490 || detail.AmountDue != 0 || detail.PaymentStatus != "paid" {
		t.Fatalf("paid money lost: %#v", detail)
	}
	if detail, err := repository.DetailForCustomer(ctx, customerID+1, number); err != nil || detail != nil {
		t.Fatalf("foreign order exposed: %#v, %v", detail, err)
	}
}
