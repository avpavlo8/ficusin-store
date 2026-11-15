package ordersale

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Called by the isolated PostgreSQL lifecycle suite, after the old publisher
// scenarios. This applies the actual cutover SQL and exercises real projections.
func testCashierReservations(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	value := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		if e := pool.QueryRow(ctx, q, args...).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	raw, e := os.ReadFile("../../../timeweb/migrations/20261115_cashier_stock_reservations.sql")
	if e != nil {
		t.Fatal(e)
	}
	t.Run("unknown existing live sale blocks the whole migration", func(t *testing.T) {
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		_, e = tx.Exec(ctx, `UPDATE order_sale_dispatch_control SET enabled=TRUE,execution_mode='live',site_warehouse_id=1,native_warehouse_id=284,movement_warehouse_id=284,organization_id=1,regulation_id=1,buyer_id=1;
 INSERT INTO orders(id,order_number,payment_method,status,delivery_method)VALUES(20010,'LEGACY-CASH-UNKNOWN','online','new','pickup');
 INSERT INTO order_items VALUES(20010,20010,1,'Legacy counter fixture','D13',1,10);`)
		if e != nil {
			t.Fatal(e)
		}
		event, e := EnqueueCreated(ctx, tx, 20010)
		if e != nil || event.IntentID == 0 {
			t.Fatal(event, e)
		}
		_, e = tx.Exec(ctx, `UPDATE orders SET payment_method='on_delivery' WHERE id=20010`)
		if e != nil {
			t.Fatal(e)
		}
		_, e = tx.Exec(ctx, `UPDATE order_sale_publications SET create_attempted=TRUE WHERE intent_id=$1`, event.IntentID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, string(raw)); e == nil {
			t.Fatal("unknown old native creation was silently converted to a cashier reserve")
		}
	})
	t.Run("queued old cash publication cannot call the provider", func(t *testing.T) {
		if e := RefreshDispatchControl(ctx, pool, DispatchControl{Mode: "mock", Enabled: true, SiteWarehouseID: 1}); e != nil {
			t.Fatal(e)
		}
		exec(`INSERT INTO orders(id,order_number,payment_method,status,delivery_method)VALUES(20009,'LEGACY-CASH-QUEUED','online','new','pickup');INSERT INTO order_items VALUES(20009,20009,1,'Legacy cash fixture','D13',1,10)`)
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		event, e := EnqueueCreated(ctx, tx, 20009)
		if e != nil {
			tx.Rollback(ctx)
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		exec(`UPDATE orders SET payment_method='on_delivery' WHERE id=20009`)
		exec(`UPDATE order_sale_publications SET next_attempt_at=NOW()+INTERVAL '1 hour' WHERE intent_id<>$1`, event.IntentID)
		exec(`UPDATE order_sale_publications SET next_attempt_at=NOW()-INTERVAL '1 day' WHERE intent_id=$1`, event.IntentID)
		calls := 0
		did, e := PublicationStep(ctx, pool, publicationProviderFunc(func(context.Context, Request, Progress) (Result, error) { calls++; return Result{}, nil }), "mock")
		if e != nil || !did || calls != 0 {
			t.Fatal("old cash publication reached warehouse provider", did, calls, e)
		}
		var state string
		if e = pool.QueryRow(ctx, `SELECT status FROM order_sale_publications WHERE intent_id=$1`, event.IntentID).Scan(&state); e != nil || state != "needs_attention" {
			t.Fatal("old publication was not held for review", state, e)
		}
		exec(`UPDATE orders SET status='cancelled' WHERE id=20009`)
	})
	exec(`UPDATE warehouses SET saby_id='different' WHERE id=2;
 INSERT INTO product_variants(id,saby_id)VALUES(20001,'20001');
 INSERT INTO saby_nomenclature(saby_id)VALUES('20001');
 INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at,stock_observed_at) VALUES(1,20001,10,0,now(),now());
 INSERT INTO orders(id,order_number,payment_method,payment_status,status,delivery_method)VALUES
 (20001,'CASH-OPEN','on_delivery','on_delivery','ready','pickup'),
 (20002,'CASH-OLD-DONE','on_delivery','on_delivery','completed','pickup'),
 (20003,'CASH-OLD-CANCEL','on_delivery','on_delivery','cancelled','pickup');
 INSERT INTO order_items VALUES(20001,20001,20001,'Counter fixture','D13',2,10),
 (20002,20002,20001,'Old issued fixture','D13',3,10),(20003,20003,20001,'Old cancelled fixture','D13',4,10);`)
	exec(string(raw))
	free := func() int64 {
		return value(`SELECT available_qty FROM order_sale_inventory WHERE warehouse_id=1 AND variant_id=20001`)
	}
	if free() != 8 || value(`SELECT COUNT(*) FROM cashier_stock_reservations`) != 1 || value(`SELECT available_qty FROM inventory WHERE variant_id=20001`) != 10 {
		t.Fatal("cutover changed physical stock or enrolled historical completed/cancelled orders")
	}
	if e := RefreshDispatchControl(ctx, pool, DispatchControl{Mode: "mock", Enabled: true, SiteWarehouseID: 1}); e != nil {
		t.Fatal(e)
	}
	for _, stage := range []string{"created", "manual", "automatic"} {
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		var event Event
		if stage == "created" {
			event, e = EnqueueCreated(ctx, tx, 20001)
		} else {
			event, e = Enqueue(ctx, tx, 20001, 0, stage)
		}
		if e != nil {
			tx.Rollback(ctx)
			t.Fatal(e)
		}
		if event.IntentID != 0 || event.CanPrepare {
			tx.Rollback(ctx)
			t.Fatal("cash checkout or handover prepared a second stock sale", stage, event)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
	}
	if value(`SELECT COUNT(*) FROM order_sale_intents WHERE order_id=20001`) != 0 {
		t.Fatal("cash order entered realization queue")
	}
	if _, e := pool.Exec(ctx, `INSERT INTO order_sale_intents(event_key,order_id,source,snapshot)VALUES('obsolete-writer:cash',20001,'automatic','{}')`); e == nil {
		t.Fatal("an old writer bypassed cashier ownership in the database")
	}
	if value(`SELECT available_qty+order_sale_owned_quantity(20001,1,20001) FROM order_sale_inventory WHERE variant_id=20001`) != 10 {
		t.Fatal("own cash reservation blocked fulfilment")
	}

	// Disabling the external realization worker must not release counter stock.
	if e := RefreshDispatchControl(ctx, pool, DispatchControl{Mode: "disabled"}); e != nil {
		t.Fatal(e)
	}
	exec(`INSERT INTO orders(id,order_number,payment_method,payment_status,status,delivery_method)VALUES(20004,'CASH-NEXT','on_delivery','on_delivery','new','pickup');
 INSERT INTO order_items VALUES(20004,20004,20001,'Next customer','D13',2,10);`)
	if free() != 6 {
		t.Fatal("disabled dispatcher disabled cashier reservation")
	}
	before := value(`SELECT COUNT(*) FROM cashier_stock_layers WHERE reservation_id=(SELECT id FROM cashier_stock_reservations WHERE order_id=20001)`)
	exec(`UPDATE order_items SET quantity=3 WHERE id=20001;UPDATE order_items SET quantity=3 WHERE id=20001`)
	if free() != 5 || value(`SELECT COUNT(*) FROM cashier_stock_layers WHERE reservation_id=(SELECT id FROM cashier_stock_reservations WHERE order_id=20001)`) != before+1 {
		t.Fatal("edit duplicated reservation or lost added units")
	}
	exec(`UPDATE order_items SET quantity=2 WHERE id=20001`)
	if free() != 6 || value(`SELECT SUM(released_quantity) FROM cashier_stock_layers WHERE reservation_id=(SELECT id FROM cashier_stock_reservations WHERE order_id=20001)`) != 1 {
		t.Fatal("quantity decrease did not release newest unit")
	}
	exec(`UPDATE orders SET status='cancelled' WHERE id=20004`)
	if free() != 8 || value(`SELECT available_qty FROM inventory WHERE variant_id=20001`) != 10 {
		t.Fatal("cancellation moved physical stock instead of releasing reserve")
	}

	// Receipt is issued on the separate till before the manager confirms the
	// paid handover. A stock upload started earlier cannot remove the local hold.
	exec(`UPDATE orders SET payment_status='paid',status='completed' WHERE id=20001`)
	if free() != 8 {
		t.Fatal("completion immediately released an unreflected counter sale")
	}
	exec(`UPDATE inventory SET synced_at=now(),stock_observed_at=(SELECT completed_at-INTERVAL '1 microsecond' FROM cashier_stock_reservations WHERE order_id=20001) WHERE variant_id=20001`)
	if free() != 8 {
		t.Fatal("late upload of an old snapshot released the cashier hold")
	}
	exec(`UPDATE inventory SET available_qty=8,stock_observed_at=(SELECT completed_at+INTERVAL '1 second' FROM cashier_stock_reservations WHERE order_id=20001) WHERE variant_id=20001`)
	if free() != 8 {
		t.Fatal("cashier write-off was deducted a second time after fresh import")
	}
	var paidAt string
	if e := pool.QueryRow(ctx, `SELECT completed_at::TEXT FROM cashier_stock_reservations WHERE order_id=20001`).Scan(&paidAt); e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE orders SET status='completed' WHERE id=20001`)
	var again string
	if e := pool.QueryRow(ctx, `SELECT completed_at::TEXT FROM cashier_stock_reservations WHERE order_id=20001`).Scan(&again); e != nil || again != paidAt {
		t.Fatal("repeat handover moved stock observation boundary", e)
	}
	if _, e := pool.Exec(ctx, `UPDATE orders SET status='new' WHERE id=20001`); e == nil {
		t.Fatal("issued order reopened and re-reserved stock")
	}
	if _, e := pool.Exec(ctx, `UPDATE orders SET status='cancelled' WHERE id=20001`); e == nil {
		t.Fatal("issued cash sale was returned without physical return")
	}
	if _, e := pool.Exec(ctx, `UPDATE order_items SET quantity=3 WHERE id=20001`); e == nil {
		t.Fatal("issued cashier composition changed without a return")
	}
}
