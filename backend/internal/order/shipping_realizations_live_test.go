package order

import (
	"context"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type realizationReadOnlyCarrier struct {
	status string
	reads  int
}

func (*realizationReadOnlyCarrier) Configured() bool { return true }
func (*realizationReadOnlyCarrier) CreateOrder(context.Context, integration.ShipmentRequest) (integration.Shipment, error) {
	panic("must not create real shipment")
}
func (*realizationReadOnlyCarrier) CancelOrder(context.Context, string) error {
	panic("must not cancel shipment")
}
func (c *realizationReadOnlyCarrier) FetchOrder(context.Context, string) (integration.Shipment, error) {
	c.reads++
	return integration.Shipment{UUID: "local", Status: c.status}, nil
}
func (c *realizationReadOnlyCarrier) FindOrderByNumber(_ context.Context, number string) (integration.Shipment, error) {
	c.reads++
	status := c.status
	if strings.Contains(number, "DIRECT") {
		status = "DELIVERED"
	}
	return integration.Shipment{UUID: "local-" + number, Status: status}, nil
}

func TestShippingRealizationHooksPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/ficusin_order_sale_test" || (parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1") {
		t.Fatal("requires loopback dedicated ficusin_order_sale_test")
	}
	ctx := context.Background()
	control, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(ctx)
	schema := fmt.Sprintf("sale_shipping_test_%d", time.Now().UnixNano())
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err = control.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	defer control.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(q string) {
		t.Helper()
		if _, err = pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE customers(id BIGINT PRIMARY KEY);CREATE TABLE warehouses(id BIGINT PRIMARY KEY,name TEXT,is_active INTEGER);
 CREATE TABLE product_external_ids(variant_id BIGINT,provider TEXT,id_type TEXT,status TEXT,external_id TEXT);
 CREATE TABLE product_variants(id BIGINT PRIMARY KEY,saby_id TEXT);INSERT INTO product_variants VALUES(1,'3980');
 CREATE TABLE orders(id BIGINT PRIMARY KEY,order_number TEXT,status TEXT,customer_id BIGINT,delivery_method TEXT,cdek_status TEXT DEFAULT '',created_at TIMESTAMPTZ DEFAULT now(),cdek_uuid TEXT DEFAULT '',cdek_track_number TEXT DEFAULT '',cdek_status_reason TEXT DEFAULT '',cdek_create_state TEXT DEFAULT 'unknown',cdek_attempts INT DEFAULT 0,cdek_next_attempt_at TIMESTAMPTZ,cdek_synced_at TIMESTAMPTZ,cdek_order_number TEXT DEFAULT '');
 CREATE TABLE order_items(id BIGINT PRIMARY KEY,order_id BIGINT,variant_id BIGINT,product_name TEXT,sku TEXT,quantity INTEGER,unit_price NUMERIC(12,2));
 CREATE TABLE shipment_offers(id BIGINT PRIMARY KEY,order_id BIGINT,status TEXT,delivery_method TEXT,cdek_status TEXT DEFAULT '',created_at TIMESTAMPTZ DEFAULT now(),updated_at TIMESTAMPTZ DEFAULT now(),cdek_uuid TEXT DEFAULT '',cdek_track_number TEXT DEFAULT '',cdek_status_reason TEXT DEFAULT '',cdek_create_state TEXT DEFAULT 'unknown',cdek_attempts INT DEFAULT 0,cdek_next_attempt_at TIMESTAMPTZ,cdek_synced_at TIMESTAMPTZ,cdek_last_error TEXT DEFAULT '');
 ALTER TABLE orders ADD COLUMN cdek_last_error TEXT DEFAULT '';
 CREATE TABLE shipment_offer_items(order_item_id BIGINT,shipment_offer_id BIGINT,variant_id BIGINT,product_name TEXT,sku TEXT,quantity INTEGER,unit_price NUMERIC(12,2));
 INSERT INTO orders(id,order_number,status,delivery_method)VALUES(1,'LOCAL-MAIN','ready','cdek'),(2,'LOCAL-PART','new','cdek'),(3,'LOCAL-DIRECT','ready','cdek');
 INSERT INTO order_items VALUES(1,1,1,'Plant','D13',2,100),(2,2,1,'Plant','D13',5,100),(3,3,1,'Plant','D13',1,100);
 INSERT INTO shipment_offers(id,order_id,status,delivery_method)VALUES(21,2,'shipping','cdek');
 INSERT INTO shipment_offer_items VALUES(2,21,1,'Exact partial','D13',2,100);`)
	migration, err := os.ReadFile("../../../timeweb/migrations/20261113_order_sale_intents.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(migration))
	dispatcherMigration, err := os.ReadFile("../../../timeweb/migrations/20261113_order_sale_publications.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(dispatcherMigration))
	buyerMigration, err := os.ReadFile("../../../timeweb/migrations/20261113_order_sale_publications_buyer.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(buyerMigration))
	lifecycleMigration, err := os.ReadFile("../../../timeweb/migrations/20261113_order_sale_publications_lifecycle.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(lifecycleMigration))
	carrier := &realizationReadOnlyCarrier{status: "IN_TRANSIT"}
	worker := &ShippingWorker{pool: pool, cdek: carrier, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	// Historical shipped rows still awaiting UUID reconciliation are manual-only.
	exec(`INSERT INTO orders(id,order_number,status,delivery_method)VALUES(6,'LOCAL-OLD-UNKNOWN','shipped','cdek');INSERT INTO order_items VALUES(6,6,1,'Historical unknown','D13',1,100);`)
	worker.reconcileUnknownShipments(ctx)
	var count, quantity int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*),COALESCE(SUM(l.quantity),0) FROM order_sale_intents i JOIN order_sale_intent_lines l ON l.intent_id=i.id`).Scan(&count, &quantity); err != nil || count != 2 || quantity != 4 {
		t.Fatalf("reconciled real handovers lost exact intents: %d/%d %v", count, quantity, err)
	}
	var direct int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_intents WHERE order_id=3`).Scan(&direct); err != nil || direct != 0 {
		t.Fatal("direct completed delivery enqueued", direct, err)
	}
	var historical int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_intents WHERE order_id=6`).Scan(&historical); err != nil || historical != 0 {
		t.Fatal("unknown historical shipped order silently backfilled", historical, err)
	}
	worker.reconcileUnknownShipments(ctx)
	carrier.status = "DELIVERED"
	worker.refreshStatuses(ctx)
	worker.refreshOfferStatuses(ctx)
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_intents`).Scan(&count); err != nil || count != 2 {
		t.Fatal("completion duplicated reconciliation intent", count, err)
	}
	// Normal polling path must also atomically enqueue a newly confirmed handover.
	exec(`INSERT INTO orders(id,order_number,status,delivery_method,cdek_uuid,cdek_create_state)VALUES(4,'LOCAL-POLL','ready','cdek','poll','registered');INSERT INTO order_items VALUES(4,4,1,'Polling plant','D13',1,100);`)
	carrier.status = "IN_TRANSIT"
	worker.refreshStatuses(ctx)
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_intents WHERE order_id=4`).Scan(&count); err != nil || count != 1 {
		t.Fatal("polling did not queue intent", count, err)
	}
	// New carrier scans at the same shipped status cannot backfill old shipped orders.
	exec(`INSERT INTO orders(id,order_number,status,delivery_method,cdek_uuid,cdek_create_state,cdek_status)VALUES(5,'LOCAL-OLD','shipped','cdek','old','registered','READY_FOR_SHIPMENT_IN_SENDER_CITY');INSERT INTO order_items VALUES(5,5,1,'Historical plant','D13',1,100);`)
	worker.refreshStatuses(ctx)
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_intents WHERE order_id=5`).Scan(&count); err != nil || count != 0 {
		t.Fatal("old shipment silently backfilled", count, err)
	}
	t.Run("cancellation owns parent before carrier can lock offer", func(t *testing.T) {
		exec(`ALTER TABLE orders ADD COLUMN cdek_cancel_state TEXT DEFAULT '';
 ALTER TABLE shipment_offers ADD COLUMN checkout_locked_until TIMESTAMPTZ;
 CREATE TABLE outbox(sent_at TIMESTAMPTZ,cancelled_at TIMESTAMPTZ,cancel_reason TEXT,lock_token TEXT,locked_until TIMESTAMPTZ,shipment_offer_id BIGINT);
 INSERT INTO warehouses VALUES(1,'fixture-main',1);UPDATE order_sale_dispatch_control SET enabled=TRUE,execution_mode='mock',site_warehouse_id=1;
 INSERT INTO orders(id,order_number,status,delivery_method)VALUES(81,'LOCAL-RACE','ready','cdek');INSERT INTO order_items VALUES(81,81,1,'Race fixture','D13',1,100);`)
		seed, beginErr := pool.Begin(ctx)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		if _, e := ordersale.EnqueueCreated(ctx, seed, 81); e != nil {
			seed.Rollback(ctx)
			t.Fatal(e)
		}
		if e := seed.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		exec(`INSERT INTO shipment_offers(id,order_id,status,delivery_method,cdek_status,cdek_uuid,cdek_create_state)VALUES(811,81,'offered','cdek','CREATED','fixture-carrier','registered');INSERT INTO shipment_offer_items VALUES(81,811,1,'Race fixture','D13',1,100)`)
		carrierCfg := cfg.Copy()
		carrierCfg.MaxConns = 1
		carrierPool, e := pgxpool.NewWithConfig(ctx, carrierCfg)
		if e != nil {
			t.Fatal(e)
		}
		defer carrierPool.Close()
		var pid int32
		if e = carrierPool.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
			t.Fatal(e)
		}
		raceCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		cancelTx, e := pool.Begin(raceCtx)
		if e != nil {
			t.Fatal(e)
		}
		defer cancelTx.Rollback(ctx)
		if _, e = cancelTx.Exec(raceCtx, `SELECT id FROM orders WHERE id=81 FOR UPDATE;SELECT pg_advisory_xact_lock(hashtextextended('order-sale:81',0))`); e != nil {
			t.Fatal(e)
		}
		finished := make(chan error, 1)
		go func() {
			localWorker := &ShippingWorker{pool: carrierPool}
			_, e := localWorker.storeShipmentStatus(raceCtx, 0, 811, true, `UPDATE shipment_offers SET status='shipped',cdek_status='IN_TRANSIT' WHERE id=$1`, int64(811))
			finished <- e
		}()
		// Wait for the actual PostgreSQL lock wait rather than guessing a delay.
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		waiting := false
		for !waiting {
			select {
			case e := <-finished:
				t.Fatalf("carrier crossed the held parent boundary: %v", e)
			case <-raceCtx.Done():
				t.Fatal("carrier never reached parent lock wait")
			case <-ticker.C:
				var isWaiting bool
				if e = pool.QueryRow(raceCtx, `SELECT COALESCE(wait_event_type='Lock',FALSE) FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&isWaiting); e != nil {
					t.Fatal(e)
				}
				waiting = isWaiting
			}
		}
		var offer int64
		if e = cancelTx.QueryRow(raceCtx, `SELECT id FROM shipment_offers WHERE id=811 FOR UPDATE NOWAIT`).Scan(&offer); e != nil {
			t.Fatalf("carrier held offer before acquiring parent/order-sale locks: %v", e)
		}
		if e = ordersale.RequestCancellation(raceCtx, cancelTx, 81); e != nil {
			t.Fatal(e)
		}
		if e = CancelOpenShipmentOffers(raceCtx, cancelTx, 81); e != nil {
			t.Fatal(e)
		}
		if _, e = cancelTx.Exec(raceCtx, `UPDATE orders SET status='cancelled' WHERE id=81`); e != nil {
			t.Fatal(e)
		}
		if e = cancelTx.Commit(raceCtx); e != nil {
			t.Fatal(e)
		}
		select {
		case e = <-finished:
			if e != nil {
				t.Fatal("carrier failed after cancellation released parent", e)
			}
		case <-raceCtx.Done():
			t.Fatal("carrier and cancellation deadlocked")
		}
		var status, cdek string
		var intents int
		var wanted bool
		if e = pool.QueryRow(ctx, `SELECT s.status,s.cdek_status,(SELECT COUNT(*) FROM order_sale_intents WHERE order_id=81),(SELECT cancellation_requested_at IS NOT NULL FROM order_sale_intents WHERE order_id=81) FROM shipment_offers s WHERE id=811`).Scan(&status, &cdek, &intents, &wanted); e != nil || status != "shipped" || cdek != "IN_TRANSIT" || intents != 1 || !wanted {
			t.Fatalf("physical status/owned cancellation lost: %s %s %d %v %v", status, cdek, intents, wanted, e)
		}
	})
}
