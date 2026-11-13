package ordersale

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"
)

func TestRealizationIntentsPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" {
		t.Fatal("requires loopback PostgreSQL")
	}
	if parsed.Path != "/ficusin_order_sale_test" {
		t.Fatal("requires dedicated ficusin_order_sale_test")
	}
	ctx := context.Background()
	control, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(ctx)
	schema := fmt.Sprintf("sale_test_%d", time.Now().UnixNano())
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
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE inventory(id BIGSERIAL PRIMARY KEY,warehouse_id BIGINT,variant_id BIGINT,available_qty INTEGER,reserved_qty INTEGER NOT NULL DEFAULT 0,synced_at TIMESTAMPTZ);
 CREATE TABLE customers(id BIGINT PRIMARY KEY);INSERT INTO customers VALUES(1);
 CREATE TABLE warehouses(id BIGINT PRIMARY KEY,name TEXT,is_active INTEGER,saby_id TEXT DEFAULT 'saby-ryazan-main');INSERT INTO warehouses VALUES(1,'Warehouse',1),(2,'Other warehouse',1);
 CREATE TABLE orders(id BIGINT PRIMARY KEY,order_number TEXT,status TEXT,delivery_method TEXT,cdek_status TEXT,created_at TIMESTAMPTZ DEFAULT now());
 CREATE TABLE product_external_ids(variant_id BIGINT,provider TEXT,id_type TEXT,status TEXT,external_id TEXT);
 CREATE TABLE products(id BIGINT PRIMARY KEY,saby_fields TEXT[]);INSERT INTO products VALUES(1,ARRAY['stock']);
 CREATE TABLE saby_nomenclature(saby_id TEXT PRIMARY KEY,missing_since TIMESTAMPTZ);INSERT INTO saby_nomenclature VALUES('3980',NULL),('3981',NULL),('4000',NULL);
 CREATE TABLE product_variants(id BIGINT PRIMARY KEY,saby_id TEXT,product_id BIGINT DEFAULT 1);INSERT INTO product_variants VALUES(1,'3980'),(2,NULL);
 CREATE TABLE order_items(id BIGINT PRIMARY KEY,order_id BIGINT,variant_id BIGINT,product_name TEXT,sku TEXT,quantity INTEGER,unit_price NUMERIC(12,2));
 CREATE TABLE shipment_offers(id BIGINT PRIMARY KEY,order_id BIGINT,status TEXT,delivery_method TEXT,cdek_status TEXT,created_at TIMESTAMPTZ DEFAULT now());
 CREATE TABLE shipment_offer_items(order_item_id BIGINT,shipment_offer_id BIGINT,variant_id BIGINT,product_name TEXT,sku TEXT,quantity INTEGER,unit_price NUMERIC(12,2));`)
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
	for _, name := range []string{"20261113_order_sale_publications_lifecycle.sql", "20261113_order_sale_zz_stock_visibility.sql"} {
		raw, e := os.ReadFile("../../../timeweb/migrations/" + name)
		if e != nil {
			t.Fatal(e)
		}
		exec(string(raw))
	}

	add := func(id int64, method, status, carrier string, variant int64, qty int) {
		t.Helper()
		exec(`INSERT INTO orders(id,order_number,status,delivery_method,cdek_status) VALUES($1,$2,$3,$4,$5)`, id, fmt.Sprintf("LOCAL-%d", id), status, method, carrier)
		exec(`INSERT INTO order_items VALUES($1,$1,$2,'Exact plant','D13',$3,1234.56)`, id, variant, qty)
	}
	enqueue := func(id, offer int64, source string) Event {
		t.Helper()
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		event, e := Enqueue(ctx, tx, id, offer, source)
		if e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		return event
	}
	t.Run("delivery JSONB snapshot survives and completion toggles cannot duplicate", func(t *testing.T) {
		add(10, "cdek", "shipped", "RECEIVED_AT_SHIPMENT_WAREHOUSE", 1, 2)
		first := enqueue(10, 0, "automatic")
		if first.IntentID == 0 {
			t.Fatal("handover not queued")
		}
		stored, e := readEvent(ctx, pool, 10, 0)
		if e != nil || !stored.CanPrepare || stored.CDEKStatus == "" || stored.Lines[0].UnitPrice != "1234.56" {
			t.Fatalf("lost frozen snapshot: %+v / %v", stored, e)
		}
		prepared, e := Prepare(ctx, pool, 1, PrepareInput{WarehouseID: 1, Events: []Selection{{OrderID: 10, Fingerprint: stored.Fingerprint}}})
		if e != nil || len(prepared) != 1 || prepared[0].State != "prepared" {
			t.Fatalf("prepare: %+v %v", prepared, e)
		}
		exec(`UPDATE orders SET status='completed',cdek_status='DELIVERED' WHERE id=10`)
		next := enqueue(10, 0, "automatic")
		if next.IntentID != first.IntentID {
			t.Fatal("completion created a duplicate")
		}
		again, e := Prepare(ctx, pool, 1, PrepareInput{WarehouseID: 1, Events: []Selection{{OrderID: 10, Fingerprint: stored.Fingerprint}}})
		if e != nil || again[0].IntentID != first.IntentID {
			t.Fatal("retry lost identity", e)
		}
		exec(`UPDATE orders SET status='ready' WHERE id=10`)
		exec(`UPDATE orders SET status='shipped' WHERE id=10`)
		if enqueue(10, 0, "automatic").IntentID != first.IntentID {
			t.Fatal("toggle created duplicate")
		}
	})
	t.Run("labels and completed delivery never auto enqueue and pickup requires completed", func(t *testing.T) {
		add(20, "cdek", "shipped", "CREATED", 1, 1)
		add(21, "cdek", "completed", "DELIVERED", 1, 1)
		add(22, "pickup", "shipped", "", 1, 1)
		add(23, "pickup", "completed", "", 1, 1)
		for _, id := range []int64{20, 21, 22} {
			if enqueue(id, 0, "automatic").IntentID != 0 {
				t.Fatalf("unexpected auto event %d", id)
			}
		}
		if enqueue(23, 0, "automatic").IntentID == 0 {
			t.Fatal("issued pickup not queued")
		}
		// A historical completed delivery is deliberate manual preparation only.
		if enqueue(21, 0, "manual").IntentID == 0 {
			t.Fatal("manual historical handover not available")
		}
	})
	t.Run("partial shipments use exact lines and quantities", func(t *testing.T) {
		add(30, "cdek", "new", "", 1, 3)
		exec(`INSERT INTO shipment_offers(id,order_id,status,delivery_method,cdek_status) VALUES(301,30,'shipped','cdek','IN_TRANSIT'),(302,30,'shipped','cdek','IN_TRANSIT');INSERT INTO shipment_offer_items VALUES(30,301,1,'Part one','D13',1,100),(30,302,1,'Part two','D13',2,200)`)
		a := enqueue(30, 301, "automatic")
		b := enqueue(30, 302, "automatic")
		if len(a.Lines) != 1 || len(b.Lines) != 1 || a.Lines[0].Quantity != 1 || b.Lines[0].Quantity != 2 || a.Lines[0].OrderItemID != 30 || b.Lines[0].UnitPrice != "200.00" {
			t.Fatalf("partial snapshot wrong: %+v %+v", a, b)
		}
		exec(`UPDATE shipment_offers SET status='completed',cdek_status='DELIVERED' WHERE id=301`)
		if enqueue(30, 301, "automatic").IntentID != a.IntentID {
			t.Fatal("partial completion duplicated")
		}
		if enqueue(30, 0, "automatic").IntentID != 0 {
			t.Fatal("full order claimed partial lines")
		}
		wrong, e := readEvent(ctx, pool, 10, 301)
		if e == nil || wrong.IntentID != 0 {
			t.Fatal("shipment read accepted another order", wrong, e)
		}
		if _, e = Prepare(ctx, pool, 1, PrepareInput{WarehouseID: 2, Events: []Selection{{OrderID: 10, ShipmentOfferID: 301, Fingerprint: a.Fingerprint}}}); e == nil {
			t.Fatal("wrong order accepted another shipment intent")
		}

	})
	t.Run("missing metadata queues safely but existing nonempty identity cannot be replaced", func(t *testing.T) {
		add(40, "pickup", "completed", "", 2, 1)
		blocked := enqueue(40, 0, "automatic")
		if blocked.IntentID == 0 || blocked.CanPrepare {
			t.Fatal("missing mapping must remain a visible blocked intent")
		}
		exec(`UPDATE product_variants SET saby_id='4000' WHERE id=2`)
		ready, e := readEvent(ctx, pool, 40, 0)
		if e != nil || !ready.CanPrepare || ready.Lines[0].SabyID != "4000" {
			t.Fatalf("missing mapping not hydrated: %+v %v", ready, e)
		}
		add(41, "pickup", "completed", "", 1, 1)
		frozen := enqueue(41, 0, "automatic")
		exec(`UPDATE product_variants SET saby_id='other-card' WHERE id=1`)
		changed, e := readEvent(ctx, pool, 41, 0)
		if e != nil || changed.CanPrepare || !changed.MappingConflict || changed.Lines[0].SabyID != "3980" {
			t.Fatalf("frozen identity changed: %+v %v", changed, e)
		}
		if _, e = Prepare(ctx, pool, 1, PrepareInput{WarehouseID: 1, Events: []Selection{{OrderID: 41, Fingerprint: frozen.Fingerprint}}}); e == nil {
			t.Fatal("relink silently accepted")
		}
		exec(`UPDATE product_variants SET saby_id='3980' WHERE id=1`)
	})
	t.Run("stale selections and rollback retain no partial preparation", func(t *testing.T) {
		add(50, "pickup", "completed", "", 1, 1)
		before, e := readEvent(ctx, pool, 50, 0)
		if e != nil {
			t.Fatal(e)
		}
		exec(`UPDATE order_items SET quantity=2 WHERE id=50`)
		_, e = Prepare(ctx, pool, 1, PrepareInput{WarehouseID: 1, Events: []Selection{{OrderID: 50, Fingerprint: before.Fingerprint}}})
		if e != ErrConflict {
			t.Fatal("stale composition accepted", e)
		}
		var count int
		if e = pool.QueryRow(ctx, `SELECT count(*) FROM order_sale_intents WHERE order_id=50`).Scan(&count); e != nil || count != 0 {
			t.Fatal("failed prepare left durable changes", count, e)
		}
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = Enqueue(ctx, tx, 50, 0, "automatic"); e != nil {
			t.Fatal(e)
		}
		tx.Rollback(ctx)
		if e = pool.QueryRow(ctx, `SELECT count(*) FROM order_sale_intents WHERE order_id=50`).Scan(&count); e != nil || count != 0 {
			t.Fatal("rolled back status kept intent")
		}
	})
	t.Run("concurrent preparation selects one immutable warehouse", func(t *testing.T) {
		add(60, "pickup", "completed", "", 1, 1)
		event := enqueue(60, 0, "automatic")
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for _, warehouse := range []int64{1, 2} {
			wg.Add(1)
			go func(id int64) {
				defer wg.Done()
				_, err := Prepare(ctx, pool, 1, PrepareInput{WarehouseID: id, Events: []Selection{{OrderID: 60, Fingerprint: event.Fingerprint}}})
				results <- err
			}(warehouse)
		}
		wg.Wait()
		close(results)
		success := 0
		conflict := 0
		for err := range results {
			if err == nil {
				success++
			} else if err == ErrConflict {
				conflict++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatal("warehouse preparation was not serialized", success, conflict)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM order_sale_intents WHERE order_id=60 AND state='prepared'`).Scan(&count); err != nil || count != 1 {
			t.Fatal("duplicate prepared intent", count, err)
		}
	})
	t.Run("durable publication foundation", func(t *testing.T) { testPublicationFoundation(t, pool) })
	t.Run("early creation lifecycle", func(t *testing.T) { testCreatedLifecycle(t, pool) })
	runEarlyStockVisibilityTests(t, ctx, pool)
	view, e := List(ctx, pool, "LOCAL-10", 0)
	if e != nil || len(view.Events) != 1 || view.ExternalWritesEnabled || view.Warehouses[0].NativeMappingConfirmed {
		t.Fatalf("view claims unsupported native capability: %+v / %v", view, e)
	}
}
