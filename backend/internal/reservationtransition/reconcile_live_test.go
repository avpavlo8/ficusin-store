package reservationtransition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func pilotFixture(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Path != "/ficusin_legacy_reconcile_test" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("requires disposable loopback ficusin_legacy_reconcile_test")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("legacy_pilot_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = base.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(); base.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); base.Close() })
	for _, table := range []string{"orders", "order_items", "product_variants", "inventory", "payments", "shipment_offers", "shipment_offer_items", "shipment_offer_inventory_reservations", "order_sale_intents", "order_sale_publications", "stock_movements"} {
		if _, err = pool.Exec(ctx, "CREATE TABLE "+table+" (LIKE public."+table+" INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)"); err != nil {
			t.Fatal(err)
		}
	}
	// The pilot deliberately tests a pre-cutover copy, never weakens current
	// public zero-state barriers or an application's production schema.
	pilotExec(t, pool, `ALTER TABLE inventory DROP CONSTRAINT IF EXISTS inventory_legacy_reservation_zero;ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_legacy_reservation_zero;ALTER TABLE shipment_offer_items DROP CONSTRAINT IF EXISTS shipment_offer_items_legacy_reservation_zero;ALTER TABLE shipment_offer_inventory_reservations DROP CONSTRAINT IF EXISTS shipment_offer_inventory_reservations_retired;`)
	raw, err := os.ReadFile("../../../timeweb/migrations/20261114_zzzz_legacy_reservation_reconciliation.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(raw)); err != nil {
		t.Fatal(err)
	}
	pilotExec(t, pool, `INSERT INTO orders(id,order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status,payment_status)
VALUES(1,'OWN-CLOSED-LEGACY','Fixture','+70000000000','','pickup',0,500,500,'cancelled','cancelled'),
(99,'UNRELATED-FIFO','Fixture','+70000000000','','pickup',0,100,100,'confirmed','pending');
INSERT INTO product_variants(id,product_id,sku,label,base_price_minor) VALUES(1,1,'900000000000000001','Own variant',10000),(2,1,'900000000000000002','Other variant',10000);
INSERT INTO order_items(id,order_id,product_id,variant_id,sku,product_name,quantity,unit_price,reserved_qty) VALUES(1,1,1,1,'900000000000000001','Own item',5,100,2);
INSERT INTO inventory(id,warehouse_id,variant_id,available_qty,reserved_qty) VALUES(1,1,1,7,1),(2,2,1,8,1);
INSERT INTO order_sale_intents(id,event_key,order_id,source,snapshot,event_trigger) VALUES(99,'unrelated-fifo',99,'automatic','{"proof":"unchanged"}','created');`)
	return ctx, pool
}
func pilotExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}
func pilotGroup(t *testing.T, ctx context.Context, pool *pgxpool.Pool) Group {
	t.Helper()
	r, err := Audit(ctx, pool, 100)
	if err != nil || len(r.Groups) != 1 {
		t.Fatalf("audit %+v %v", r, err)
	}
	return r.Groups[0]
}

func TestLegacyReservationPilotPostgres(t *testing.T) {
	t.Run("dry run then exact idempotent release", func(t *testing.T) {
		ctx, pool := pilotFixture(t)
		g := pilotGroup(t, ctx, pool)
		if !g.Safe || g.InventoryReserved != 2 || g.UnreleasedReserved != 2 {
			t.Fatalf("safe cancelled group missing: %+v", g)
		}
		var beforeLedger, afterLedger []byte
		if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM order_sale_intents i`).Scan(&beforeLedger); err != nil {
			t.Fatal(err)
		}
		var unchanged int64
		pool.QueryRow(ctx, `SELECT SUM(reserved_qty) FROM inventory`).Scan(&unchanged)
		if unchanged != 2 {
			t.Fatal("dry-run wrote data")
		}
		result, err := ApplyLocal(ctx, pool, 1, g.Fingerprint)
		if err != nil || result.AlreadyApplied || result.ProviderWrites || result.PhysicalStockChanged {
			t.Fatalf("apply %+v %v", result, err)
		}
		repeat, err := ApplyLocal(ctx, pool, 1, g.Fingerprint)
		if err != nil || !repeat.AlreadyApplied || repeat.OperationKey != result.OperationKey {
			t.Fatalf("repeat %+v %v", repeat, err)
		}
		var reserved, available, itemQty, orderedQty, movements, quantity int64
		var released bool
		pool.QueryRow(ctx, `SELECT SUM(reserved_qty),SUM(available_qty) FROM inventory`).Scan(&reserved, &available)
		pool.QueryRow(ctx, `SELECT reserved_qty,quantity FROM order_items WHERE id=1`).Scan(&itemQty, &orderedQty)
		pool.QueryRow(ctx, `SELECT stock_released_at IS NOT NULL FROM orders WHERE id=1`).Scan(&released)
		pool.QueryRow(ctx, `SELECT COUNT(*),SUM(quantity) FROM stock_movements WHERE order_id=1 AND kind='release' AND status='local_reconciled'`).Scan(&movements, &quantity)
		if reserved != 0 || available != 15 || itemQty != 0 || orderedQty != 5 || !released || movements != 1 || quantity != 2 {
			t.Fatalf("changed physical/history or wrong release: %d/%d item=%d guard=%v journal=%d/%d", reserved, available, itemQty, released, movements, quantity)
		}
		pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM order_sale_intents i`).Scan(&afterLedger)
		if string(beforeLedger) != string(afterLedger) {
			t.Fatal("FIFO/realization history changed")
		}
		var before, after json.RawMessage
		if err = pool.QueryRow(ctx, `SELECT before_snapshot,after_snapshot FROM legacy_reservation_reconciliations WHERE operation_key=$1`, result.OperationKey).Scan(&before, &after); err != nil {
			t.Fatal(err)
		}
		var original, reconciled struct {
			OrderItems []struct {
				ItemID, OrderID, ReservedQty, Quantity int64
			} `json:"orderItems"`
		}
		if err = json.Unmarshal(before, &original); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(after, &reconciled); err != nil {
			t.Fatal(err)
		}
		if len(original.OrderItems) != 1 || original.OrderItems[0].ItemID != 1 || original.OrderItems[0].OrderID != 1 || original.OrderItems[0].ReservedQty != 2 || original.OrderItems[0].Quantity != 5 || len(reconciled.OrderItems) != 1 || reconciled.OrderItems[0].ItemID != 1 || reconciled.OrderItems[0].OrderID != 1 || reconciled.OrderItems[0].ReservedQty != 0 || reconciled.OrderItems[0].Quantity != 5 {
			t.Fatalf("reservation attribution or ordered quantity lost: before=%+v after=%+v", original, reconciled)
		}
		if _, err = pool.Exec(ctx, `DELETE FROM legacy_reservation_reconciliations`); err == nil {
			t.Fatal("reconciliation receipt mutable")
		}
	})
	t.Run("reconciled group passes zero state cutover", func(t *testing.T) {
		ctx, pool := pilotFixture(t)
		g := pilotGroup(t, ctx, pool)
		result, err := ApplyLocal(ctx, pool, 1, g.Fingerprint)
		if err != nil {
			t.Fatal(err)
		}
		var schema string
		if err = pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile("../../../timeweb/migrations/20261114_zzzzz_legacy_reservation_cutover.sql")
		if err != nil {
			t.Fatal(err)
		}
		// Rehearse the real barrier only on this historical fixture's copies.
		qualified := pgx.Identifier{schema}.Sanitize()
		sql := strings.ReplaceAll(string(raw), "public.", qualified+".")
		sql = strings.ReplaceAll(sql, "legacy_archive", qualified)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, sql); err != nil {
			t.Fatalf("safe reconciliation still blocks cutover: %v", err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		repeat, err := ApplyLocal(ctx, pool, 1, g.Fingerprint)
		if err != nil || !repeat.AlreadyApplied || repeat.OperationKey != result.OperationKey {
			t.Fatalf("cutover broke receipt-based idempotence: %+v %v", repeat, err)
		}
	})
	t.Run("concurrent same operation", func(t *testing.T) {
		ctx, pool := pilotFixture(t)
		g := pilotGroup(t, ctx, pool)
		var wg sync.WaitGroup
		out := make(chan Result, 2)
		errs := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() { defer wg.Done(); r, e := ApplyLocal(ctx, pool, 1, g.Fingerprint); out <- r; errs <- e }()
		}
		wg.Wait()
		close(out)
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		applied := 0
		for r := range out {
			if !r.AlreadyApplied {
				applied++
			}
		}
		if applied != 1 {
			t.Fatal("concurrent writers both applied")
		}
		var reserved, ordered, count, released int64
		if err := pool.QueryRow(ctx, `SELECT (SELECT SUM(reserved_qty) FROM inventory)+(SELECT SUM(reserved_qty) FROM order_items), (SELECT quantity FROM order_items WHERE id=1), (SELECT COUNT(*) FROM legacy_reservation_reconciliations), (SELECT SUM(quantity) FROM stock_movements WHERE kind='release')`).Scan(&reserved, &ordered, &count, &released); err != nil || reserved != 0 || ordered != 5 || count != 1 || released != 2 {
			t.Fatalf("concurrent result not zero-cutover ready: reserved=%d ordered=%d receipts=%d released=%d err=%v", reserved, ordered, count, released, err)
		}
	})
	t.Run("snapshot changes stop apply", func(t *testing.T) {
		ctx, pool := pilotFixture(t)
		g := pilotGroup(t, ctx, pool)
		pilotExec(t, pool, `UPDATE orders SET status='confirmed' WHERE id=1`)
		if _, err := ApplyLocal(ctx, pool, 1, g.Fingerprint); !errors.Is(err, ErrChanged) {
			t.Fatalf("changed plan applied: %v", err)
		}
		var n int
		pool.QueryRow(ctx, `SELECT count(*) FROM legacy_reservation_reconciliations`).Scan(&n)
		if n != 0 {
			t.Fatal("changed plan got a receipt")
		}
	})
	for _, c := range []struct{ name, sql, reason string }{
		{"active", `UPDATE orders SET status='confirmed' WHERE id=1`, "active_order"},
		{"payment unknown", `INSERT INTO payments(id,order_id,idempotence_key,amount,status) VALUES(1,1,'unknown-payment',500,'pending')`, "payment_outcome_unresolved"},
		{"local cancelled payment still needs GET", `INSERT INTO payments(id,order_id,idempotence_key,provider_payment_id,amount,status) VALUES(1,1,'cancelled-payment','known-provider-id',500,'cancelled')`, "payment_readback_requires_separate_review"},
		{"carrier unknown", `UPDATE orders SET delivery_method='cdek',cdek_create_state='unknown' WHERE id=1`, "carrier_cancellation_unresolved"},
		{"ledger cancellation unknown", `INSERT INTO order_sale_intents(id,event_key,order_id,source,snapshot,event_trigger) VALUES(1,'own-unknown',1,'automatic','{}','created')`, "realization_cancellation_unresolved"},
		{"quantity mismatch", `UPDATE inventory SET reserved_qty=0 WHERE id=2`, "reservation_quantity_mismatch"},
		{"multi variant", `INSERT INTO order_items(id,order_id,product_id,variant_id,sku,product_name,quantity,unit_price,reserved_qty) VALUES(2,1,1,2,'900000000000000002','Other item',1,100,1)`, "multi_variant_ownership"},
		{"offer allocation", `INSERT INTO shipment_offers(id,order_id,public_token,order_revision,status,delivery_method) VALUES(1,1,'own-offer',1,'cancelled','pickup');INSERT INTO shipment_offer_items(id,shipment_offer_id,order_item_id,variant_id,product_name,sku,quantity,unit_price) VALUES(1,1,1,1,'Own item','900000000000000001',1,100);INSERT INTO shipment_offer_inventory_reservations(shipment_offer_item_id,inventory_id,quantity) VALUES(1,1,1)`, "offer_warehouse_attribution_requires_review"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, pool := pilotFixture(t)
			pilotExec(t, pool, c.sql)
			report, err := Audit(ctx, pool, 100)
			if err != nil {
				t.Fatal(err)
			}
			var g Group
			for _, candidate := range report.Groups {
				if candidate.VariantID == 1 {
					g = candidate
				}
			}
			found := false
			for _, b := range g.Blockers {
				found = found || b == c.reason
			}
			if g.Safe || !found {
				t.Fatalf("blocker missing %+v", g)
			}
			if _, err = ApplyLocal(ctx, pool, 1, g.Fingerprint); !errors.Is(err, ErrBlocked) {
				t.Fatalf("unsafe group applied: %v", err)
			}
			var reserved int64
			pool.QueryRow(ctx, `SELECT SUM(reserved_qty) FROM inventory`).Scan(&reserved)
			if c.name != "quantity mismatch" && reserved != 2 {
				t.Fatal("blocked reconciliation changed another owner's reserve")
			}
		})
	}
}

type getOnlyPaymentReader struct{ calls []string }

func (r *getOnlyPaymentReader) FetchPayment(_ context.Context, id string) (integration.Payment, error) {
	r.calls = append(r.calls, id)
	p := integration.Payment{ID: id, Amount: 100, Status: "canceled"}
	switch id {
	case "pending":
		p.Status = "pending"
	case "authorization":
		p.Status = "waiting_for_capture"
	case "paid":
		p.Status = "succeeded"
		p.Paid = true
	case "wrong-id":
		p.ID = "other"
	case "wrong-amount":
		p.Amount = 99.99
	}
	return p, nil
}
func TestPaymentReadOnlyObservationPostgres(t *testing.T) {
	ctx, pool := pilotFixture(t)
	for i, remote := range []string{"", "cancelled", "pending", "authorization", "paid", "wrong-id", "wrong-amount"} {
		pilotExec(t, pool, `INSERT INTO payments(id,order_id,idempotence_key,provider_payment_id,amount,status) VALUES($1,1,$2,$3,100,'superseded')`, i+1, fmt.Sprint(i), remote)
	}
	var before, after []byte
	pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM payments p`).Scan(&before)
	reader := &getOnlyPaymentReader{}
	result, err := ReadUnfinishedPayments(ctx, pool, reader, 100)
	if err != nil {
		t.Fatal(err)
	}
	if result.Checked != 6 || result.NoProviderID != 1 || len(reader.calls) != 6 || result.Outcomes["paid_requires_financial_review"] != 1 || result.Outcomes["authorization_requires_decision_no_capture_sent"] != 1 || result.Outcomes["identity_mismatch"] != 1 || result.Outcomes["amount_mismatch"] != 1 {
		t.Fatalf("unsafe observations: %+v", result)
	}
	pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM payments p`).Scan(&after)
	if string(before) != string(after) {
		t.Fatal("GET observation changed financial state")
	}
}
