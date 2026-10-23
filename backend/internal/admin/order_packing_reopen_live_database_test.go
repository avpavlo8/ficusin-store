package admin

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestPackingReopenRetiresOnlySafePaymentsOnLiveDatabase(t *testing.T) {
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
	var customerID int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$1,'Reopen operator',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("reopen-actor-%d@example.invalid", time.Now().UnixNano())).Scan(&customerID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, customerID)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customerID)
	}()
	repo := NewPostgresRepository(pool)
	actor := Actor{CustomerID: customerID, Role: RoleOwner}
	for _, scenario := range []string{"pending", "paid", "legacy", "unknown", "ambiguous", "shipped", "stale_revision", "creating", "forbidden"} {
		t.Run(scenario, func(t *testing.T) {
			key := fmt.Sprintf("REOPEN-%d-%s", time.Now().UnixNano(), scenario)
			var id, w int64
			must := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			must(pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,address,delivery_method,delivery_payee,subtotal,delivery_fee,total,status,cdek_uuid,cdek_create_state,cdek_status)VALUES($1,'Reopen','+70000000000','','','cdek','shop',1000,680,1680,'new','old-uuid','registered','CREATED') RETURNING id`, key).Scan(&id))
			defer func() {
				_, _ = pool.Exec(ctx, `DELETE FROM outbox WHERE order_packing_id IN(SELECT id FROM order_packing_workflows WHERE order_id=$1)`, id)
				_, _ = pool.Exec(ctx, `DELETE FROM payments WHERE order_id=$1`, id)
				_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id)
			}()
			must(pool.QueryRow(ctx, `INSERT INTO order_packing_workflows(order_id,order_revision,idempotency_key,fingerprint,boxes,tariff_code,delivery_fee,state,payment_url)VALUES($1,1,$2,'fixture','[]',136,680,'notification_queued','https://old.test')RETURNING id`, id, key).Scan(&w))
			mustExec := func(sql string, args ...any) { t.Helper(); _, e := pool.Exec(ctx, sql, args...); must(e) }
			mustExec(`INSERT INTO outbox(recipient,subject,body,order_packing_id)VALUES('reopen@example.invalid','old','old',$1)`, w)
			paymentStatus := "pending"
			if scenario == "paid" || scenario == "unknown" {
				paymentStatus = scenario
			}
			payload := `{"Metadata":{"ficusin_capture_mode":"two_stage"}}`
			if scenario == "legacy" {
				payload = `{}`
			}
			mustExec(`INSERT INTO payments(order_id,idempotence_key,amount,status,provider_payment_id,request_payload)VALUES($1,$2,1680,$3,'provider-old',$4)`, id, key, paymentStatus, payload)
			if scenario == "ambiguous" {
				mustExec(`UPDATE payments SET provider_payment_id='' WHERE order_id=$1`, id)
			}
			if scenario == "shipped" {
				mustExec(`UPDATE orders SET status='shipped' WHERE id=$1`, id)
			}
			if scenario == "creating" {
				mustExec(`UPDATE orders SET cdek_create_state='creating' WHERE id=$1`, id)
			}
			revision := 1
			if scenario == "stale_revision" {
				revision = 2
			}
			who := actor
			if scenario == "forbidden" {
				who.Role = "customer"
			}
			state, e := repo.RequestOrderPackingReopen(ctx, who, id, revision)
			safe := scenario == "pending" || scenario == "paid"
			if !safe {
				if e == nil {
					t.Fatal("unsafe reopen accepted")
				}
				return
			}
			must(e)
			if state.State != "reopening" || state.PaymentURL != "" || !state.DeliveryFeePending {
				t.Fatalf("unexpected state %+v", state)
			}
			_, e = repo.RequestOrderPackingReopen(ctx, actor, id, revision)
			must(e)
			var ps string
			must(pool.QueryRow(ctx, `SELECT status FROM payments WHERE order_id=$1`, id).Scan(&ps))
			want := "superseded"
			if scenario == "paid" {
				want = "paid"
			}
			if ps != want {
				t.Fatalf("payment %s wanted %s", ps, want)
			}
			var unsent bool
			must(pool.QueryRow(ctx, `SELECT cancelled_at IS NOT NULL FROM outbox WHERE order_packing_id=$1`, w).Scan(&unsent))
			if !unsent {
				t.Fatal("old notification remains active")
			}
			if _, e = repo.EditOrder(ctx, actor, id, OrderEdit{}); e == nil {
				t.Fatal("composition unlocked before carrier removal")
			}
		})
	}
}
