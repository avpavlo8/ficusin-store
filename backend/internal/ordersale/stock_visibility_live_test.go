package ordersale

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Called by the guarded dedicated-schema fixture; no provider is constructed.
func runEarlyStockVisibilityTests(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	t.Run("early stock priority and delayed snapshots", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, e := tx.Exec(ctx, sql, args...); e != nil {
				t.Fatal(e)
			}
		}
		const variant = 9100001
		exec(`INSERT INTO product_variants(id,saby_id) VALUES($1,'9100001')`, int64(variant))
		t0 := time.Now().UTC().Add(-time.Hour)
		t1 := t0.Add(time.Minute)
		exec(`INSERT INTO inventory(id,warehouse_id,variant_id,available_qty,reserved_qty,synced_at,stock_observed_at) VALUES($1,1,$1,1,0,NOW(),$2)`, int64(variant), t0)
		exec(`UPDATE order_sale_dispatch_control SET execution_mode='live',enabled=TRUE,site_warehouse_id=1,native_warehouse_id=283,movement_warehouse_id=284,organization_id=259,regulation_id=122498,buyer_id=18655 WHERE singleton`)
		create := func(id int64) Event {
			t.Helper()
			exec(`INSERT INTO orders(id,order_number,status,delivery_method,cdek_status) VALUES($1,$2,'new','pickup','')`, id, fmt.Sprintf("STOCK-%d", id))
			exec(`INSERT INTO order_items(id,order_id,variant_id,product_name,sku,quantity,unit_price) VALUES($1,$1,$2,'fixture','stock-fixture',1,10)`, id, int64(variant))
			event, e := EnqueueCreated(ctx, tx, id)
			if e != nil || event.IntentID == 0 {
				t.Fatalf("create: %+v %v", event, e)
			}
			return event
		}
		first := create(9100001)
		check := func(orderID int64, wantPublic, wantOwn int) {
			t.Helper()
			var public, own int
			e := tx.QueryRow(ctx, `SELECT GREATEST(available_qty-reserved_qty,0),GREATEST(available_qty-reserved_qty+order_sale_owned_quantity($1,warehouse_id,variant_id),0) FROM order_sale_inventory WHERE warehouse_id=1 AND variant_id=$2`, orderID, int64(variant)).Scan(&public, &own)
			if e != nil || public != wantPublic || own != wantOwn {
				t.Fatalf("order %d: public/own=%d/%d want=%d/%d err=%v", orderID, public, own, wantPublic, wantOwn, e)
			}
		}
		check(first.OrderID, 0, 1)
		second := create(9100002)
		check(first.OrderID, 0, 1)
		check(second.OrderID, 0, 0) // Later preorder cannot borrow the first plant.
		check(9999999, 0, 0)        // An unrelated order has no credit.
		// Simulate a retained verified posting in this isolated SQL fixture.
		exec(`UPDATE order_sale_publications SET status='posted',stock_verified=TRUE,stock_verified_at=$2,document_id='9100001',document_uuid=request->>'operationKey',stock_proof='{"fixture":true}' WHERE intent_id=$1`, first.IntentID, t1)
		check(first.OrderID, 0, 1)
		// This snapshot was fetched before posting, although uploaded afterwards.
		exec(`UPDATE inventory SET available_qty=1,synced_at=NOW(),stock_observed_at=$2 WHERE variant_id=$1`, int64(variant), t0)
		check(first.OrderID, 0, 1)
		check(second.OrderID, 0, 0)
		// Actual stock already includes the deduction before our acknowledgement.
		exec(`UPDATE inventory SET available_qty=0,stock_observed_at=$2 WHERE variant_id=$1`, int64(variant), t1.Add(-time.Second))
		check(first.OrderID, 0, 1)
		check(second.OrderID, 0, 0)
		// A later collection proves the deduction is reflected: no double debit.
		exec(`UPDATE inventory SET available_qty=3,stock_observed_at=$2 WHERE variant_id=$1`, int64(variant), t1.Add(time.Second))
		check(first.OrderID, 2, 3)
		check(second.OrderID, 2, 3)
		// Asking for cancellation does not release an unknown external outcome.
		exec(`UPDATE order_sale_publications SET desired_state='cancelled' WHERE intent_id=$1`, second.IntentID)
		check(first.OrderID, 2, 3)
		exec(`UPDATE order_sale_intents SET cancellation_verified=TRUE,cancellation_verified_at=NOW() WHERE id=$1`, second.IntentID)
		// This job never reached a writer: retain an explicit verified no-remote
		// cancellation, rather than bypassing the publication's evidence.
		exec(`UPDATE order_sale_publications SET status='cancelled',cancellation_verified=TRUE,cancellation_verified_at=NOW(),cancellation_proof='{"kind":"no_remote"}' WHERE intent_id=$1`, second.IntentID)
		// The first order can use its already deducted unit plus three free ones.
		check(first.OrderID, 3, 4)
		// Mock documents never cause a real imported balance to include a debit.
		exec(`UPDATE order_sale_dispatch_control SET execution_mode='mock' WHERE singleton`)
		third := create(9100003)
		exec(`UPDATE order_sale_publications SET status='posted',stock_verified=TRUE,stock_verified_at=$2,document_id='STG-SALE-9100003',document_uuid=request->>'operationKey',stock_proof='{"fixture":true}' WHERE intent_id=$1`, third.IntentID, t0)
		check(third.OrderID, 2, 3)
	})
}
