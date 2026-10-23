package payment

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestKopeckPaymentAndSplitRefundOnLiveDatabase(t *testing.T) {
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
	number := fmt.Sprintf("CI-KOPECK-%d", time.Now().UnixNano())
	var orderID int64
	err = pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status) VALUES($1,'fixture','+70000000000','fixture@example.invalid','pickup',0,.30,.30,'online','pending','confirmed') RETURNING id`, number).Scan(&orderID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM payment_refunds WHERE order_id=$1`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, orderID)
	}()
	provider := &livePaymentProvider{}
	service := NewService(pool, provider, "https://example.invalid", slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err = pool.Exec(ctx, `INSERT INTO payments(order_id,idempotence_key,provider_payment_id,amount,status) VALUES($1,$2,$2,.30,'pending')`, orderID, number)
	if err != nil {
		t.Fatal(err)
	}
	for _, amount := range []float64{.29, .31} {
		provider.status = integration.Payment{ID: number, Status: "succeeded", Paid: true, Amount: amount}
		if err = service.SyncOutstanding(ctx, number); err != nil {
			t.Fatal(err)
		}
		balance, err := service.BalanceForOrder(ctx, orderID)
		if err != nil || balance.Paid != 0 || balance.Due != .30 {
			t.Fatalf("amount mismatch marked paid: %+v %v", balance, err)
		}
	}
	provider.status.Amount = .30
	if err = service.SyncOutstanding(ctx, number); err != nil {
		t.Fatal(err)
	}
	balance, err := service.BalanceForOrder(ctx, orderID)
	if err != nil || balance.Paid != .30 || balance.Due != 0 {
		t.Fatalf("exact cents not paid: %+v %v", balance, err)
	}
	// Two real local ledger facts, mocked provider; refund must split .20+.09.
	_, err = pool.Exec(ctx, `DELETE FROM payments WHERE order_id=$1`, orderID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO payments(order_id,idempotence_key,provider_payment_id,amount,status) VALUES($1,$2,$2,.10,'paid'),($1,$3,$3,.20,'paid')`, orderID, number+"-a", number+"-b")
	if err != nil {
		t.Fatal(err)
	}
	requestCtx := WithRefundRequest(ctx, "kopeck-split-idempotence")
	for range 2 {
		balance, err = service.RefundAmount(requestCtx, orderID, .29, "fixture")
		if err != nil || balance.Refunded != .29 || balance.NetPaid != .01 || provider.refunds != 2 {
			t.Fatalf("split or duplicate: %+v count=%d %v", balance, provider.refunds, err)
		}
	}
	if _, err = service.RefundAmount(ctx, orderID, .001, "fraction"); err == nil || provider.refunds != 2 {
		t.Fatal("fractional kopeck reached provider")
	}
	balance, err = service.RefundAmount(WithRefundRequest(ctx, "kopeck-final-idempotence"), orderID, .01, "fixture")
	if err != nil || balance.Refunded != .30 || balance.NetPaid != 0 || provider.refunds != 3 {
		t.Fatalf("last kopeck lost: %+v count=%d %v", balance, provider.refunds, err)
	}
}
