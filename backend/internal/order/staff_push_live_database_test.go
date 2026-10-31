package order

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/webpush"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStaffOrderPushOnLiveDatabase(t *testing.T) {
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
	for _, scenario := range []string{"manager", "administrator", "owner", "customer", "inactive", "expired_subscription", "no_subscription", "revoked", "reassigned", "unsubscribed", "retry", "gone", "old", "rollback"} {
		t.Run(scenario, func(t *testing.T) {
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			key := fmt.Sprintf("staff-push-%d", time.Now().UnixNano())
			var customer, order, subscription int64
			must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Push fixture',CURRENT_TIMESTAMP) RETURNING id`, key+"@example.invalid", key).Scan(&customer))
			defer func() {
				_, _ = pool.Exec(ctx, `DELETE FROM admin_users WHERE customer_id=$1`, customer)
				_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, order)
				_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customer)
			}()
			role := scenario
			if role != "owner" && role != "administrator" {
				role = "manager"
			}
			if scenario != "customer" {
				_, err = pool.Exec(ctx, `INSERT INTO admin_users(email,customer_id,role,is_active) VALUES($1,$2,$3,$4)`, key+"@example.invalid", customer, role, scenario != "inactive")
				must(err)
			}
			must(pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status) VALUES($1,'fixture',$1,'','pickup',0,690,690,'cash','pending','new') RETURNING id`, key).Scan(&order))
			if scenario != "no_subscription" {
				must(pool.QueryRow(ctx, `INSERT INTO push_subscriptions(customer_id,endpoint,p256dh,auth,expired_at) VALUES($1,$2,'key','auth',CASE WHEN $3 THEN CURRENT_TIMESTAMP ELSE NULL END) RETURNING id`, customer, "https://fcm.googleapis.com/"+key, scenario == "expired_subscription").Scan(&subscription))
			}
			tx, err := pool.Begin(ctx)
			must(err)
			must(queueStaffOrderPush(ctx, tx, order))
			must(queueStaffOrderPush(ctx, tx, order))
			if scenario == "rollback" {
				must(tx.Rollback(ctx))
			} else {
				must(tx.Commit(ctx))
			}
			var count int
			must(pool.QueryRow(ctx, `SELECT COUNT(*) FROM staff_order_push_outbox WHERE order_id=$1`, order).Scan(&count))
			excluded := scenario == "customer" || scenario == "inactive" || scenario == "expired_subscription" || scenario == "no_subscription" || scenario == "rollback"
			if excluded {
				if count != 0 {
					t.Fatalf("unexpected queue entries %d", count)
				}
				return
			}
			if count != 1 {
				t.Fatalf("queue entries %d", count)
			}
			switch scenario {
			case "revoked":
				_, err = pool.Exec(ctx, `UPDATE admin_users SET is_active=false WHERE customer_id=$1`, customer)
			case "reassigned":
				_, err = pool.Exec(ctx, `UPDATE push_subscriptions SET customer_id=NULL WHERE id=$1`, subscription)
			case "unsubscribed":
				_, err = pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE id=$1`, subscription)
			case "old":
				_, err = pool.Exec(ctx, `UPDATE staff_order_push_outbox SET created_at=CURRENT_TIMESTAMP-INTERVAL '25 hours' WHERE order_id=$1`, order)
			}
			must(err)
			sender := &packingPushStub{}
			if scenario == "retry" {
				sender.err = errors.New("provider unavailable")
			}
			if scenario == "gone" {
				sender.err = webpush.ErrGone
			}
			worker := NewStaffOrderPushWorker(pool, sender, quietLogger())
			worker.process(ctx)
			if scenario == "unsubscribed" {
				if len(sender.messages) != 0 {
					t.Fatal("sent after unsubscribe")
				}
				return
			}
			if scenario == "retry" {
				var attempts int
				var done *time.Time
				must(pool.QueryRow(ctx, `SELECT attempts,completed_at FROM staff_order_push_outbox WHERE order_id=$1`, order).Scan(&attempts, &done))
				if attempts != 1 || done != nil {
					t.Fatal("failed send marked complete")
				}
				sender.err = nil
				_, err = pool.Exec(ctx, `UPDATE staff_order_push_outbox SET next_attempt_at=CURRENT_TIMESTAMP WHERE order_id=$1`, order)
				must(err)
				worker.process(ctx)
			}
			worker.process(ctx)
			var done *time.Time
			var outcome string
			must(pool.QueryRow(ctx, `SELECT completed_at,outcome FROM staff_order_push_outbox WHERE order_id=$1`, order).Scan(&done, &outcome))
			expected := 1
			want := "sent"
			switch scenario {
			case "revoked", "reassigned":
				expected = 0
				want = "inactive"
			case "old":
				expected = 0
				want = "expired"
			case "retry":
				expected = 2
			case "gone":
				want = "gone"
			}
			if done == nil || outcome != want || len(sender.messages) != expected {
				t.Fatalf("done=%v outcome=%s sends=%d", done, outcome, len(sender.messages))
			}
			if expected > 0 {
				m := sender.messages[0]
				if m.URL != fmt.Sprintf("/admin?section=orders&order=%d", order) || m.Title != "Новый заказ "+key {
					t.Fatalf("wrong notification %+v", m)
				}
			}
			if scenario == "gone" {
				var expired *time.Time
				must(pool.QueryRow(ctx, `SELECT expired_at FROM push_subscriptions WHERE id=$1`, subscription).Scan(&expired))
				if expired == nil {
					t.Fatal("dead subscription still enabled")
				}
			}
		})
	}
}
