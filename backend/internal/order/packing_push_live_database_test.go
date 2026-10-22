package order

import (
	"context"
	"errors"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/notify"
	"github.com/avpavlo8/ficusin-store/backend/internal/webpush"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

type packingPushStub struct {
	err      error
	messages []notify.Message
}

func (*packingPushStub) PublicKey() string { return "configured" }
func (s *packingPushStub) Deliver(_ context.Context, _ notify.Subscription, m notify.Message) error {
	s.messages = append(s.messages, m)
	return s.err
}
func TestPackingPushIndependentOfMailOnLiveDatabase(t *testing.T) {
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
	for _, scenario := range []string{"smtp_failure", "no_email", "retry", "cancelled", "expired_subscription", "unsubscribed", "no_subscription"} {
		t.Run(scenario, func(t *testing.T) {
			key := fmt.Sprintf("packing-push-%d", time.Now().UnixNano())
			email := key + "@example.invalid"
			var customer, order, workflow, subscription int64
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Push fixture',CURRENT_TIMESTAMP) RETURNING id`, email, key).Scan(&customer))
			if scenario == "no_email" {
				email = ""
			}
			must(pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,delivery_payee,delivery_fee,subtotal,total,payment_method,payment_status,status,cdek_create_state,cdek_track_number,created_at) VALUES($1,$2,'Push fixture',$1,$3,'cdek','shop',0,690,690,'online','pending','confirmed','registered','TRACK',CURRENT_TIMESTAMP-INTERVAL '30 days') RETURNING id`, key, customer, email).Scan(&order))
			must(pool.QueryRow(ctx, `INSERT INTO order_packing_workflows(order_id,order_revision,idempotency_key,fingerprint,boxes,tariff_code,delivery_fee) VALUES($1,1,$2,'fixture','[]',136,0) RETURNING id`, order, key).Scan(&workflow))
			defer func() {
				_, _ = pool.Exec(ctx, `DELETE FROM outbox WHERE order_packing_id=$1`, workflow)
				_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, order)
				_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customer)
			}()
			if scenario != "no_subscription" {
				must(pool.QueryRow(ctx, `INSERT INTO push_subscriptions(customer_id,endpoint,p256dh,auth) VALUES($1,$2,'fixture-key','fixture-auth') RETURNING id`, customer, "https://fcm.googleapis.com/"+key).Scan(&subscription))
			}
			prepare := NewPackingWorker(pool, &packingPaymentStub{}, "https://shop.example.test", quietLogger())
			must(prepare.advance(ctx, workflow))
			must(prepare.advance(ctx, workflow))
			var count int
			var prepared *time.Time
			must(pool.QueryRow(ctx, `SELECT prepared_at FROM order_packing_workflows WHERE id=$1`, workflow).Scan(&prepared))
			if prepared == nil {
				t.Fatal("mail blocked preparation")
			}
			must(pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_packing_push_outbox WHERE workflow_id=$1`, workflow).Scan(&count))
			if scenario == "no_subscription" {
				if count != 0 {
					t.Fatal("invented subscription")
				}
				return
			}
			if count != 1 {
				t.Fatalf("delivery count %d", count)
			}
			if scenario == "smtp_failure" {
				NewLetterWorker(pool, &packingSenderStub{fail: true}, quietLogger()).process(ctx)
			}
			if scenario == "cancelled" {
				_, err = pool.Exec(ctx, `UPDATE orders SET status='cancelled' WHERE id=$1`, order)
				must(err)
			}
			if scenario == "unsubscribed" {
				_, err = pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE id=$1`, subscription)
				must(err)
			}
			sender := &packingPushStub{}
			if scenario == "retry" {
				sender.err = errors.New("push provider unavailable")
			}
			if scenario == "expired_subscription" {
				sender.err = webpush.ErrGone
			}
			worker := NewPackingPushWorker(pool, sender, quietLogger())
			worker.process(ctx)
			var sent, notified *time.Time
			var attempts int
			must(pool.QueryRow(ctx, `SELECT sent_at,attempts FROM order_packing_push_outbox WHERE workflow_id=$1`, workflow).Scan(&sent, &attempts))
			if scenario == "cancelled" || scenario == "unsubscribed" {
				if len(sender.messages) != 0 || sent != nil {
					t.Fatal("sent to invalidated recipient")
				}
				return
			}
			if scenario == "expired_subscription" {
				if sent != nil {
					t.Fatal("expired reported sent")
				}
				return
			}
			if scenario == "retry" {
				if sent != nil || attempts != 1 {
					t.Fatal("failure claimed delivery")
				}
				sender.err = nil
				_, err = pool.Exec(ctx, `UPDATE order_packing_push_outbox SET next_attempt_at=CURRENT_TIMESTAMP WHERE workflow_id=$1`, workflow)
				must(err)
				worker.process(ctx)
			}
			worker.process(ctx)
			must(pool.QueryRow(ctx, `SELECT p.sent_at,w.notified_at FROM order_packing_push_outbox p JOIN order_packing_workflows w ON w.id=p.workflow_id WHERE w.id=$1`, workflow).Scan(&sent, &notified))
			if sent == nil || notified == nil {
				t.Fatal("push depends on email")
			}
			expected := 1
			if scenario == "retry" {
				expected = 2
			}
			if len(sender.messages) != expected {
				t.Fatalf("sends %d", len(sender.messages))
			}
			if sender.messages[0].URL != "/account/orders/"+key {
				t.Fatal("missing order link")
			}
			expiry := NewExpiryWorker(pool, packingExpirySettings{}, nil, quietLogger())
			must(expiry.cancel(ctx, order))
			var status string
			must(pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, order).Scan(&status))
			if status != "confirmed" {
				t.Fatal("payment window backdated")
			}
		})
	}
}
