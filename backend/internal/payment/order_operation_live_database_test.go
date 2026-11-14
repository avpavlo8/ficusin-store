package payment

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOutstandingPaymentSerializationOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	number := fmt.Sprintf("PACKING-PAY-LOCK-%d", time.Now().UnixNano())
	var id int64
	if err = pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status) VALUES($1,'Local concurrency fixture','+70000000000','packing-concurrency@example.invalid','pickup',0,690,690,'online','pending','confirmed') RETURNING id`, number).Scan(&id); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM orders WHERE id=$1`, id) }()
	provider := &livePaymentProvider{}
	service := NewService(pool, provider, "https://ficusin.example.invalid", slog.New(slog.NewTextHandler(io.Discard, nil)))
	// A manager holds the same lock while storing a new approved total.
	guard, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.Rollback(context.Background()) }()
	if _, err = guard.Exec(ctx, `SELECT pg_advisory_xact_lock(7241,$1::integer)`, id); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			paymentURL, _, requestErr := service.StartOutstanding(ctx, number)
			if requestErr == nil && paymentURL != "https://payments.example.invalid/ci" {
				requestErr = fmt.Errorf("unexpected payment URL %q", paymentURL)
			}
			results <- requestErr
		}()
	}
	select {
	case result := <-results:
		t.Fatalf("issued payment before preparation committed: %v", result)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err = guard.Exec(ctx, `UPDATE orders SET total=1530,delivery_fee=840 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = guard.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	provider.mu.Lock()
	creates, amount := provider.creates, provider.request.Amount
	provider.mu.Unlock()
	var count int
	var storedAmount float64
	if err = pool.QueryRow(ctx, `SELECT COUNT(*),MAX(amount)::double precision FROM payments WHERE order_id=$1 AND status='pending'`, id).Scan(&count, &storedAmount); err != nil {
		t.Fatal(err)
	}
	if creates != 1 || count != 1 || amount != 1530 || storedAmount != 1530 {
		t.Fatalf("payment requests were not serialized: creates=%d count=%d provider=%.2f stored=%.2f", creates, count, amount, storedAmount)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO order_packing_workflows(order_id,order_revision,idempotency_key,fingerprint,boxes,tariff_code,delivery_fee) VALUES($1,1,$2,'fixture','[]',136,840)`, id, number); err != nil {
		t.Fatal(err)
	}
	if err = service.SupersedePending(ctx, id); err == nil {
		t.Fatal("late editor retired frozen packing payment")
	}
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM payments WHERE order_id=$1 AND status='pending'`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("confirmed packing payment was modified: %d %v", count, err)
	}

	if _, err = pool.Exec(ctx, `UPDATE payments SET amount=840 WHERE order_id=$1 AND status='pending'`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status) VALUES($1,$2,690,'paid')`, id, number+"-plants-paid"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE orders SET payment_status='partially_paid' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.StartOutstanding(ctx, number); err != nil {
		t.Fatalf("customer could not reuse delivery-only top-up: %v", err)
	}
	provider.mu.Lock()
	creates = provider.creates
	provider.mu.Unlock()
	if creates != 1 {
		t.Fatal("customer reopening top-up created another payment for plants")
	}

}

func TestOrderStatusFollowsFullPaymentOnLiveDatabase(t *testing.T) {
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
	service := NewService(pool, nil, "https://ficusin.example.invalid", slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tc := range []struct {
		name, before, want   string
		paid                 float64
		feePending, preorder int
	}{
		{"unpaid", "confirmed", "confirmed", 0, 0, 0},
		{"partial", "confirmed", "confirmed", 690, 0, 0},
		// A paid balance alone is not a fulfilment proof. These fixtures have
		// no order lines, current Saby stock, or confirmed CDEK packaging.
		{"paid-without-composition", "confirmed", "confirmed", 1530, 0, 0},
		{"paid-new-without-composition", "new", "new", 1530, 0, 0},
		{"ready", "ready", "ready", 1530, 0, 0},
		{"in-transit", "shipped", "shipped", 1530, 0, 0},
		{"completed", "completed", "completed", 1530, 0, 0},
		{"cancelled", "cancelled", "cancelled", 1530, 0, 0},
		{"delivery-pending", "confirmed", "confirmed", 1530, 1, 0},
		{"stock-pending", "confirmed", "confirmed", 1530, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			number := fmt.Sprintf("STATUS-%s-%d", tc.name, time.Now().UnixNano())
			var id int64
			if err := pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status,delivery_fee_pending,has_preorder) VALUES($1,'Status fixture','+70000000000','status@example.invalid','cdek',0,1530,1530,'online','pending',$2,$3,$4) RETURNING id`, number, tc.before, tc.feePending, tc.preorder).Scan(&id); err != nil {
				t.Fatal(err)
			}
			defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id) }()
			if tc.paid > 0 {
				if _, err := pool.Exec(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status) VALUES($1,$2,$3,'paid')`, id, number, tc.paid); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := service.Reconcile(ctx, id); err != nil {
				t.Fatal(err)
			}
			var got string
			if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, id).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("status %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDiscountedOrderPaymentUsesDiscountedTotalOnLiveDatabase(t *testing.T) {
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
	number := fmt.Sprintf("DISCOUNT-PAY-%d", time.Now().UnixNano())
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status,manager_discount_type,manager_discount_value,manager_discount_amount) VALUES($1,'Discount fixture','+70000000000','discount@example.invalid','pickup',840,621,1461,'online','pending','confirmed','percent',10,69) RETURNING id`, number).Scan(&id); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id) }()
	provider := &livePaymentProvider{}
	service := NewService(pool, provider, "https://ficusin.example.invalid", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, _, err := service.StartOutstanding(ctx, number); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	total := 0.0
	for _, line := range provider.request.Items {
		total += line.Price * float64(line.Quantity)
	}
	if provider.request.Amount != 1461 || total != 1461 {
		t.Fatalf("discount lost in payment: amount %.2f receipt %.2f", provider.request.Amount, total)
	}
}

func TestOutstandingSyncRejectsWrongIdentityAndAmountOnLiveDatabase(t *testing.T) {
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
	number := fmt.Sprintf("CURRENT-SYNC-%d", time.Now().UnixNano())
	var id int64
	if err = pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status) VALUES($1,'Sync fixture','+70000000000','sync@example.invalid','pickup',0,100,100,'online','pending','confirmed') RETURNING id`, number).Scan(&id); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id)
	remote := number + "-provider"
	if _, err = pool.Exec(ctx, `INSERT INTO payments(order_id,idempotence_key,provider_payment_id,amount,status) VALUES($1,$2,$2,100,'pending')`, id, remote); err != nil {
		t.Fatal(err)
	}
	provider := &livePaymentProvider{status: integration.Payment{ID: "different-payment", Status: "succeeded", Paid: true, Amount: 100}}
	service := NewService(pool, provider, "https://fixture.invalid", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err = service.SyncOutstanding(ctx, remote); err == nil {
		t.Fatal("provider identity mismatch accepted")
	}
	var state string
	var recorded bool
	if err = pool.QueryRow(ctx, `SELECT o.payment_status||':'||p.status,p.paid_at IS NOT NULL FROM orders o JOIN payments p ON p.order_id=o.id WHERE o.id=$1`, id).Scan(&state, &recorded); err != nil || state != "pending:pending" || recorded {
		t.Fatalf("foreign payment changed local state: %s/%v %v", state, recorded, err)
	}
	provider.status = integration.Payment{ID: remote, Status: "succeeded", Paid: true, Amount: 99.99}
	if err = service.SyncOutstanding(ctx, remote); err != nil {
		t.Fatal(err)
	}
	balance, err := service.BalanceForOrder(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if balance.Paid != 0 || balance.Due != 100 || balance.PaymentStatus != StatusPending {
		t.Fatalf("wrong amount credited to order: %+v", balance)
	}
	if err = pool.QueryRow(ctx, `SELECT paid_at IS NOT NULL FROM payments WHERE order_id=$1`, id).Scan(&recorded); err != nil || recorded {
		t.Fatalf("wrong amount marked paid: %v %v", recorded, err)
	}
}
