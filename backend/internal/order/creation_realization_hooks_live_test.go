package order

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type creationRealizationSettings struct{}

func (creationRealizationSettings) Number(key string) int {
	if key == settings.AutoCancelHours {
		return 24
	}
	return 0
}

type expiryPaymentRaceCanceller func(context.Context, int64) error

func (f expiryPaymentRaceCanceller) CancelPending(ctx context.Context, orderID int64) error {
	return f(ctx, orderID)
}

// Exercises the real checkout and expiry entry points in a unique schema. No
// external client is present; only the future-only durable queue is asserted.
func TestCreationRealizationHooksPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Path != "/ficusin_order_sale_test" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") {
		t.Fatal("requires loopback dedicated ficusin_order_sale_test")
	}
	ctx := context.Background()
	control, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(ctx)
	schema := fmt.Sprintf("sale_checkout_test_%d", time.Now().UnixNano())
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
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE customers(id BIGINT PRIMARY KEY,full_name TEXT,retail_discount_bps INTEGER);
 CREATE TABLE warehouses(id BIGINT PRIMARY KEY,name TEXT,is_active INTEGER,saby_id TEXT);INSERT INTO warehouses VALUES(1,'Test',1,'saby-ryazan-main');
 CREATE TABLE products(id BIGINT PRIMARY KEY,name TEXT,status TEXT,saby_fields TEXT[]);INSERT INTO products VALUES(1,'Exact test plant','published',ARRAY['stock']);
 CREATE TABLE product_variants(id BIGINT PRIMARY KEY,product_id BIGINT,saby_id TEXT,sku TEXT,label TEXT,base_price_minor BIGINT,is_active INTEGER,archived_at TIMESTAMPTZ);INSERT INTO product_variants VALUES(1,1,'3980','1','D13',123456,1,NULL);
 CREATE TABLE product_external_ids(variant_id BIGINT,provider TEXT,id_type TEXT,status TEXT,external_id TEXT);
 CREATE TABLE saby_nomenclature(saby_id TEXT PRIMARY KEY,missing_since TIMESTAMPTZ);INSERT INTO saby_nomenclature VALUES('3980',NULL);
 CREATE TABLE inventory(id BIGSERIAL PRIMARY KEY,warehouse_id BIGINT,variant_id BIGINT,available_qty INTEGER,reserved_qty INTEGER,synced_at TIMESTAMPTZ);INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at)VALUES(1,1,10,0,now());
 CREATE TABLE launch_hidden_extra_plant_variants(variant_id BIGINT);
 CREATE FUNCTION variant_numeric_attribute(BIGINT,TEXT)RETURNS NUMERIC LANGUAGE SQL IMMUTABLE AS 'SELECT 13::NUMERIC';
 CREATE FUNCTION shipping_package_dimension(BIGINT,TEXT)RETURNS NUMERIC LANGUAGE SQL IMMUTABLE AS 'SELECT 20::NUMERIC';
 CREATE TABLE orders(id BIGSERIAL PRIMARY KEY,order_number TEXT UNIQUE,customer_id BIGINT,customer_name TEXT,phone TEXT,email TEXT,address TEXT,comment TEXT,
 delivery_method TEXT,delivery_fee NUMERIC,delivery_fee_pending INTEGER DEFAULT 0,delivery_repack_requested INTEGER DEFAULT 0,cdek_city_code INTEGER,cdek_city_name TEXT,cdek_office_code TEXT,cdek_tariff_code INTEGER,subtotal NUMERIC,total NUMERIC,
 payment_method TEXT,payment_status TEXT,has_preorder INTEGER DEFAULT 0,status TEXT,delivery_payee TEXT,cdek_checkout_requested INTEGER,guest_payment_token_hash TEXT,cdek_status TEXT DEFAULT '',cdek_uuid TEXT DEFAULT '',cdek_create_state TEXT DEFAULT 'new',cdek_cancel_state TEXT DEFAULT '',created_at TIMESTAMPTZ DEFAULT now(),stock_released_at TIMESTAMPTZ,cancellation_reason TEXT);
 CREATE TABLE order_items(id BIGSERIAL PRIMARY KEY,order_id BIGINT,product_id BIGINT,variant_id BIGINT,sku TEXT,product_name TEXT,variant_label TEXT,variant_snapshot JSONB,unit_price NUMERIC(12,2),quantity INTEGER,is_preorder INTEGER,reserved_qty INTEGER);
 CREATE TABLE shipment_offers(id BIGINT PRIMARY KEY,order_id BIGINT,status TEXT,delivery_method TEXT,cdek_status TEXT,cdek_uuid TEXT DEFAULT '',cdek_create_state TEXT DEFAULT 'new',created_at TIMESTAMPTZ DEFAULT now(),checkout_locked_until TIMESTAMPTZ,updated_at TIMESTAMPTZ);
 CREATE TABLE shipment_offer_items(order_item_id BIGINT,shipment_offer_id BIGINT,variant_id BIGINT,product_name TEXT,sku TEXT,quantity INTEGER,unit_price NUMERIC(12,2));
 CREATE TABLE procurement_requests(kind TEXT,saby_id TEXT,requested_name TEXT,quantity INTEGER,customer_order_id BIGINT,status TEXT,notes TEXT);
 CREATE TABLE consent_events(customer_id BIGINT,order_id BIGINT,event TEXT,documents TEXT,document_version TEXT,phone TEXT,ip_address TEXT,user_agent TEXT);
 CREATE TABLE outbox(recipient TEXT,subject TEXT,body TEXT,shipment_offer_id BIGINT,sent_at TIMESTAMPTZ,cancelled_at TIMESTAMPTZ,cancel_reason TEXT,lock_token TEXT,locked_until TIMESTAMPTZ);
 CREATE TABLE push_subscriptions(id BIGINT PRIMARY KEY,customer_id BIGINT,expired_at TIMESTAMPTZ);
 CREATE TABLE admin_users(customer_id BIGINT,is_active BOOLEAN,role TEXT);
 CREATE TABLE staff_order_push_outbox(order_id BIGINT,subscription_id BIGINT,customer_id BIGINT,UNIQUE(order_id,subscription_id));
 CREATE TABLE order_packing_workflows(id BIGINT,order_id BIGINT,state TEXT,notified_at TIMESTAMPTZ);
 CREATE TABLE payments(id BIGINT,order_id BIGINT,status TEXT,updated_at TIMESTAMPTZ);
 CREATE TABLE stock_movements(order_id BIGINT,variant_id BIGINT,saby_id TEXT,kind TEXT,quantity INTEGER,status TEXT);`)
	migrations, err := filepath.Glob("../../../timeweb/migrations/*order_sale*.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(migrations)
	for _, path := range migrations {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		exec(string(raw))
	}
	service := NewService(pool, nil, nil, creationRealizationSettings{}, quietLogger())
	checkout := func(method string) int64 {
		t.Helper()
		created, err := service.Create(ctx, CreateInput{Customer: CustomerInput{Name: "Synthetic checkout", Phone: "+79990000001", Email: "test@example.invalid"}, Delivery: "pickup", Items: []ItemInput{{ID: "1", Quantity: 2}}, PaymentMethod: method, OnlinePaymentReady: true, Consent: true, OfferAccepted: true})
		if err != nil {
			t.Fatal(err)
		}
		var id int64
		if err = pool.QueryRow(ctx, `SELECT id FROM orders WHERE order_number=$1`, created.OrderNumber).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	historical := checkout("online") // disabled at creation: later enabling must not backfill.
	if err = ordersale.RefreshDispatchControl(ctx, pool, ordersale.DispatchControl{Mode: "mock", Enabled: true, SiteWarehouseID: 1}); err != nil {
		t.Fatal(err)
	}
	id := checkout("online")
	var count int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_publications p JOIN order_sale_intents i ON i.id=p.intent_id WHERE i.order_id=$1 AND p.execution_mode='mock'`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("checkout did not atomically enqueue one future-only job", count, err)
	}
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_intents WHERE order_id=$1`, historical).Scan(&count); err != nil || count != 0 {
		t.Fatal("historical checkout was backfilled", count, err)
	}
	// The last two plants belong to this order even after its early hold made
	// public availability zero. An older/unowned order cannot borrow that hold.
	exec(`UPDATE inventory SET available_qty=2 WHERE variant_id=1`)
	shipping := &ShippingWorker{pool: pool}
	if short, err := shipping.orderStockShort(ctx, id); err != nil || short {
		t.Fatal("own early deduction blocked fulfilment", short, err)
	}
	if short, err := shipping.orderStockShort(ctx, historical); err != nil || !short {
		t.Fatal("unowned order borrowed another order's hold", short, err)
	}
	exec(`UPDATE inventory SET available_qty=10 WHERE variant_id=1`)
	exec(`UPDATE orders SET created_at=now()-INTERVAL '25 hours' WHERE id=$1`, id)
	worker := NewExpiryWorker(pool, creationRealizationSettings{}, nil, quietLogger())
	if err = worker.cancel(ctx, id); err != nil {
		t.Fatal(err)
	}
	var status, desired string
	if err = pool.QueryRow(ctx, `SELECT o.status,p.desired_state FROM orders o JOIN order_sale_intents i ON i.order_id=o.id JOIN order_sale_publications p ON p.intent_id=i.id WHERE o.id=$1`, id).Scan(&status, &desired); err != nil || status != "cancelled" || desired != "cancelled" {
		t.Fatal("expiry lost durable undo intent", status, desired, err)
	}
	// A capture can win after the worker selected an eligible unpaid order.
	// CancelPending skips an already-paid attempt, so the final locked order
	// check must prevent both cancellation and a queued physical stock undo.
	paidRace := checkout("online")
	exec(`UPDATE orders SET created_at=now()-INTERVAL '25 hours' WHERE id=$1`, paidRace)
	providerCalls := 0
	racingWorker := NewExpiryWorker(pool, creationRealizationSettings{}, expiryPaymentRaceCanceller(func(ctx context.Context, orderID int64) error {
		providerCalls++
		if orderID != paidRace {
			return fmt.Errorf("wrong race order")
		}
		_, err := pool.Exec(ctx, `UPDATE orders SET payment_status='paid' WHERE id=$1`, orderID)
		return err
	}), quietLogger())
	if err = racingWorker.cancel(ctx, paidRace); err != nil {
		t.Fatal(err)
	}
	var paymentStatus string
	var undoRequested, released bool
	var retainedQuantity int
	if err = pool.QueryRow(ctx, `SELECT o.status,o.payment_status,p.desired_state,i.cancellation_requested_at IS NOT NULL,o.stock_released_at IS NOT NULL,(SELECT SUM(quantity)::INTEGER FROM order_sale_intent_lines WHERE intent_id=i.id) FROM orders o JOIN order_sale_intents i ON i.order_id=o.id JOIN order_sale_publications p ON p.intent_id=i.id WHERE o.id=$1`, paidRace).Scan(&status, &paymentStatus, &desired, &undoRequested, &released, &retainedQuantity); err != nil || providerCalls != 1 || status != "new" || paymentStatus != "paid" || desired != "posted" || undoRequested || released || retainedQuantity != 2 {
		t.Fatal("capture racing expiry cancelled a paid order or released its hold", status, paymentStatus, desired, undoRequested, released, retainedQuantity, providerCalls, err)
	}
	cash := checkout("on_delivery")
	exec(`UPDATE orders SET created_at=now()-INTERVAL '25 hours' WHERE id=$1`, cash)
	worker.process(ctx)
	if err = pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, cash).Scan(&status); err != nil || strings.EqualFold(status, "cancelled") {
		t.Fatal("pickup cash was automatically cancelled", status, err)
	}
	// Failure later in the checkout transaction must not leave a stock hold
	// or publication which the worker could dispatch for a rejected checkout.
	var beforeJobs int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_publications`).Scan(&beforeJobs); err != nil {
		t.Fatal(err)
	}
	exec(`ALTER TABLE outbox ADD CONSTRAINT reject_synthetic_recipient CHECK(recipient<>'reject@example.invalid')`)
	_, rejected := service.Create(ctx, CreateInput{Customer: CustomerInput{Name: "Rejected synthetic", Phone: "+79990000003", Email: "reject@example.invalid"}, Delivery: "pickup", Items: []ItemInput{{ID: "1", Quantity: 1}}, PaymentMethod: "online", OnlinePaymentReady: true, Consent: true, OfferAccepted: true})
	if rejected == nil {
		t.Fatal("expected later checkout transaction failure")
	}
	var rejectedOrders, afterJobs int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE email='reject@example.invalid'`).Scan(&rejectedOrders); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_publications`).Scan(&afterJobs); err != nil || rejectedOrders != 0 || afterJobs != beforeJobs {
		t.Fatal("failed checkout left dispatchable stock hold", rejectedOrders, beforeJobs, afterJobs, err)
	}
	// Both checkouts load the same published SKU before entering their
	// transaction. The sorted variant lock makes only the first availability
	// decision immediately fulfilable when there is one last pair of plants.
	exec(`INSERT INTO products VALUES(2,'Concurrent test plant','published',ARRAY['stock']);INSERT INTO product_variants VALUES(2,2,'4000','2','D13',123456,1,NULL);INSERT INTO saby_nomenclature VALUES('4000',NULL);INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at)VALUES(1,2,2,0,now())`)
	var group sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := service.Create(ctx, CreateInput{Customer: CustomerInput{Name: "Concurrent synthetic", Phone: "+79990000002", Email: "concurrent@example.invalid"}, Delivery: "pickup", Items: []ItemInput{{ID: "2", Quantity: 2}}, PaymentMethod: "online", OnlinePaymentReady: true, Consent: true, OfferAccepted: true})
			results <- err
		}()
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var immediate, preorder int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FILTER(WHERE o.has_preorder=0),COUNT(*) FILTER(WHERE o.has_preorder=1) FROM orders o JOIN order_items oi ON oi.order_id=o.id WHERE oi.variant_id=2`).Scan(&immediate, &preorder); err != nil || immediate != 1 || preorder != 1 {
		t.Fatal("competing checkout double-allocated stock", immediate, preorder, err)
	}

}
