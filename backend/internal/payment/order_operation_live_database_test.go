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
	for index := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var paymentURL string
			var requestErr error
			if index%2 == 0 {
				paymentURL, _, requestErr = service.StartOutstanding(ctx, number)
			} else {
				paymentURL, requestErr = service.Start(ctx, number)
			}
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
	if _, err = service.Start(ctx, number); err != nil {
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
		{"paid", "confirmed", "assembling", 1530, 0, 0},
		{"paid-new", "new", "assembling", 1530, 0, 0},
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
	if _, err := service.Start(ctx, number); err != nil {
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
