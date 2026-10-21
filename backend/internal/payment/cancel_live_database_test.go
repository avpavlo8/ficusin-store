package payment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercise the real durable request writer, not a handcrafted JSON fixture:
// encoding/json stores PaymentRequest.Metadata with a capital M.
func TestCancelPendingMetadataOnLiveDatabase(t *testing.T) {
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
	for _, test := range []struct {
		name, format, status string
		unknown, wantReview  bool
		wantState            string
		wantCancels          int
	}{
		{name: "stored Go request", format: "Metadata", status: "pending", wantState: StatusSuperseded},
		{name: "lowercase compatibility", format: "metadata", status: "pending", wantState: StatusSuperseded},
		{name: "legacy pending remains blocked", format: "legacy", status: "pending", wantReview: true, wantState: StatusPending},
		{name: "paid remains blocked", format: "Metadata", status: "succeeded", wantReview: true, wantState: StatusPending},
		{name: "unknown creation remains blocked", format: "Metadata", status: "pending", unknown: true, wantReview: true, wantState: StatusPending},
		{name: "authorized hold can cancel", format: "Metadata", status: "waiting_for_capture", wantState: StatusCancelled, wantCancels: 1},
		{name: "already canceled is idempotent", format: "Metadata", status: "canceled", wantState: StatusCancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			suffix := fmt.Sprint(time.Now().UnixNano())
			var orderID, paymentID int64
			if err := pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,address,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status) VALUES($1,'Cancel test','+70000000000','cancel@example.invalid','','pickup',0,1490,1490,'online','pending','new') RETURNING id`, "CANCEL-"+suffix).Scan(&orderID); err != nil {
				t.Fatal(err)
			}
			defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, orderID) }()
			if err := pool.QueryRow(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status) VALUES($1,$2,1490,'pending') RETURNING id`, orderID, suffix).Scan(&paymentID); err != nil {
				t.Fatal(err)
			}
			provider := &livePaymentProvider{status: integration.Payment{ID: "ci-provider-payment", Status: test.status, Amount: 1490, Paid: test.status == "succeeded"}}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			service := NewService(pool, provider, "https://ficusin.example.invalid", logger)
			if _, err := service.attemptPaymentCreation(ctx, paymentID, integration.PaymentRequest{IdempotenceKey: suffix, Amount: 1490}); err != nil {
				t.Fatal(err)
			}
			var capitalStored bool
			if err := pool.QueryRow(ctx, `SELECT request_payload ? 'Metadata' FROM payments WHERE id=$1`, paymentID).Scan(&capitalStored); err != nil || !capitalStored {
				t.Fatalf("not exercising the actual Go request representation: %v %v", capitalStored, err)
			}
			if test.format == "metadata" {
				if _, err := pool.Exec(ctx, `UPDATE payments SET request_payload=(request_payload-'Metadata')||jsonb_build_object('metadata',request_payload->'Metadata') WHERE id=$1`, paymentID); err != nil {
					t.Fatal(err)
				}
			} else if test.format == "legacy" {
				if _, err := pool.Exec(ctx, `UPDATE payments SET request_payload=request_payload-'Metadata' WHERE id=$1`, paymentID); err != nil {
					t.Fatal(err)
				}
			}
			if test.unknown {
				if _, err := pool.Exec(ctx, `UPDATE payments SET provider_payment_id='' WHERE id=$1`, paymentID); err != nil {
					t.Fatal(err)
				}
			}
			err := service.CancelPending(ctx, orderID)
			if test.wantReview {
				if !errors.Is(err, ErrPaymentNeedsReview) {
					t.Fatalf("unsafe payment must remain blocked: %v", err)
				}
			} else if err != nil {
				t.Fatalf("unpaid two-stage cancellation failed: %v", err)
			}
			var state string
			if err := pool.QueryRow(ctx, `SELECT status FROM payments WHERE id=$1`, paymentID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state != test.wantState || provider.cancels != test.wantCancels || provider.captures != 0 {
				t.Fatalf("state=%s cancels=%d captures=%d", state, provider.cancels, provider.captures)
			}
			if state == StatusSuperseded {
				// Authorization after local cancellation must be found even without a
				// webhook, beyond the usual two-hour polling window, and never captured.
				if _, err := pool.Exec(ctx, `UPDATE orders SET status='cancelled' WHERE id=$1`, orderID); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `UPDATE payments SET created_at=CURRENT_TIMESTAMP-INTERVAL '3 hours',updated_at=CURRENT_TIMESTAMP-INTERVAL '6 minutes' WHERE id=$1`, paymentID); err != nil {
					t.Fatal(err)
				}
				provider.status.Status = "waiting_for_capture"
				NewReconcileWorker(service, logger).process(ctx)
				if provider.cancels != 1 || provider.captures != 0 {
					t.Fatalf("late authorization escaped cancellation: cancels=%d captures=%d", provider.cancels, provider.captures)
				}
				if err := pool.QueryRow(ctx, `SELECT status FROM payments WHERE id=$1`, paymentID).Scan(&state); err != nil {
					t.Fatal(err)
				}
				if state != "canceled" {
					t.Fatalf("late hold was not reconciled: %s", state)
				}
				if err := service.CancelPending(ctx, orderID); err != nil {
					t.Fatal(err)
				}
				if provider.cancels != 1 {
					t.Fatal("repeat cancellation duplicated provider operation")
				}
			}
		})
	}
}
