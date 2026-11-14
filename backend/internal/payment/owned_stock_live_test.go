package payment

import (
	"context"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"io"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The inventory-function contract is isolated here; lifecycle tests verify its
// implementation separately. These execute all real payment stock queries,
// including the transactional offer route, and prove correct owner forwarding.
func TestOwnedOrderStockQueriesPostgres(t *testing.T) {
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
	schema := fmt.Sprintf("sale_payment_test_%d", time.Now().UnixNano())
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
	exec(`CREATE TABLE warehouses(id BIGINT,saby_id TEXT,is_active INTEGER);INSERT INTO warehouses VALUES(1,'saby-ryazan-main',1);
 CREATE TABLE products(id BIGINT,saby_fields TEXT[]);INSERT INTO products VALUES(1,ARRAY['stock']);
 CREATE TABLE product_variants(id BIGINT,product_id BIGINT,saby_id TEXT);INSERT INTO product_variants VALUES(1,1,'3980');
 CREATE TABLE saby_nomenclature(saby_id TEXT,missing_since TIMESTAMPTZ);INSERT INTO saby_nomenclature VALUES('3980',NULL);
 CREATE TABLE order_sale_inventory(warehouse_id BIGINT,variant_id BIGINT,available_qty INTEGER,reserved_qty INTEGER,synced_at TIMESTAMPTZ);INSERT INTO order_sale_inventory VALUES(1,1,0,0,now());
 CREATE FUNCTION order_sale_owned_quantity(BIGINT,BIGINT,BIGINT)RETURNS INTEGER LANGUAGE SQL STABLE AS 'SELECT CASE WHEN $1=10 AND $2=1 AND $3=1 THEN 2 ELSE 0 END';
 CREATE TABLE orders(id BIGINT PRIMARY KEY,status TEXT);INSERT INTO orders VALUES(10,'new'),(20,'new');
 CREATE TABLE order_items(order_id BIGINT,variant_id BIGINT,quantity INTEGER);INSERT INTO order_items VALUES(10,1,2),(20,1,2);
 CREATE TABLE shipment_offers(id BIGINT PRIMARY KEY,order_id BIGINT,status TEXT,checkout_locked_until TIMESTAMPTZ,updated_at TIMESTAMPTZ);INSERT INTO shipment_offers VALUES(101,10,'offered',NULL,NULL),(201,20,'offered',NULL,NULL);
 CREATE TABLE shipment_offer_items(shipment_offer_id BIGINT,variant_id BIGINT,quantity INTEGER);INSERT INTO shipment_offer_items VALUES(101,1,2),(201,1,2);
 CREATE TABLE payments(id BIGSERIAL PRIMARY KEY,order_id BIGINT,shipment_offer_id BIGINT,idempotence_key TEXT,amount NUMERIC,status TEXT);CREATE UNIQUE INDEX pending_order ON payments(order_id)WHERE status='pending';`)
	service := &Service{pool: pool, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err = service.checkOrderStock(ctx, 10); err != nil {
		t.Fatal("own order was blocked", err)
	}
	if err = service.checkOrderStock(ctx, 20); err == nil {
		t.Fatal("other order borrowed owned stock")
	}
	if err = service.checkOfferStock(ctx, 101); err != nil {
		t.Fatal("offer did not resolve its owning order", err)
	}
	if err = service.checkOfferStock(ctx, 201); err == nil {
		t.Fatal("other offer borrowed owned stock")
	}
	exec(`UPDATE order_sale_inventory SET synced_at=now()-INTERVAL '3 hours'`)
	if err = service.checkOrderStock(ctx, 10); err == nil {
		t.Fatal("own credit bypassed freshness")
	}
	exec(`UPDATE order_sale_inventory SET synced_at=now()`)
	if _, err = service.reserveOfferAndCreatePayment(ctx, 201, 20, "foreign", 2469.12); err == nil {
		t.Fatal("transactional other offer borrowed owned stock")
	}
	if id, err := service.reserveOfferAndCreatePayment(ctx, 101, 10, "own", 2469.12); err != nil || id <= 0 {
		t.Fatal("transactional own offer rejected", id, err)
	}
	exec(`ALTER TABLE orders ADD COLUMN order_number TEXT DEFAULT 'OWNED',ADD COLUMN delivery_payee TEXT DEFAULT 'shop',ADD COLUMN subtotal NUMERIC DEFAULT 2469.12,ADD COLUMN total NUMERIC DEFAULT 2469.12,ADD COLUMN delivery_fee_pending INTEGER DEFAULT 0,ADD COLUMN has_preorder INTEGER DEFAULT 0,ADD COLUMN payment_method TEXT DEFAULT 'online',ADD COLUMN email TEXT DEFAULT '',ADD COLUMN phone TEXT DEFAULT '';
 ALTER TABLE payments ADD COLUMN last_error TEXT DEFAULT '',ADD COLUMN updated_at TIMESTAMPTZ;
 CREATE TABLE payment_refunds(order_id BIGINT,amount NUMERIC,status TEXT);
 CREATE TABLE test_revision_wait(order_id BIGINT);INSERT INTO test_revision_wait VALUES(10);
 CREATE FUNCTION order_sale_revision_pending(BIGINT)RETURNS BOOLEAN LANGUAGE SQL STABLE AS 'SELECT EXISTS(SELECT 1 FROM test_revision_wait WHERE order_id=$1)';`)
	provider := &livePaymentProvider{status: integration.Payment{ID: "held-owned", Status: "waiting_for_capture", Amount: 2469.12}}
	service.provider = provider
	held := provider.status
	exec(`UPDATE order_sale_inventory SET available_qty=-3`)
	if err = service.resolveAuthorization(ctx, held, 1, 10, nil, 2469.12, StatusPending); err != nil || provider.captures != 0 || provider.cancels != 0 {
		t.Fatal("pending revision transient stock captured or cancelled valid authorization", provider.captures, provider.cancels, err)
	}
	exec(`UPDATE orders SET status='cancelled' WHERE id=10`)
	if err = service.resolveAuthorization(ctx, held, 1, 10, nil, 2469.12, StatusPending); err != nil || provider.captures != 0 || provider.cancels != 1 {
		t.Fatal("revision wait hid authoritative order cancellation", provider.captures, provider.cancels, err)
	}
	provider.cancels = 0
	exec(`UPDATE orders SET status='new' WHERE id=10;DELETE FROM test_revision_wait WHERE order_id=10;UPDATE order_sale_inventory SET available_qty=3`)
	if err = service.resolveAuthorization(ctx, held, 1, 10, nil, 2469.12, StatusPending); err != nil || provider.captures != 1 || provider.cancels != 0 {
		t.Fatal("settled revision did not allow capture", provider.captures, provider.cancels, err)
	}

}

// A succeeded provider fact must bypass the revision gate entirely. A nil DB
// makes any accidental lookup in this path fail the test rather than hide it.
func TestRevisionGuardNeverReclassifiesSucceededPayment(t *testing.T) {
	provider := &livePaymentProvider{}
	service := &Service{provider: provider}
	err := service.resolveAuthorization(context.Background(), integration.Payment{ID: "paid", Status: "succeeded", Paid: true, Amount: 100}, 1, 10, nil, 100, StatusPending)
	if err != nil || provider.captures != 0 || provider.cancels != 0 {
		t.Fatal("actual paid fact was reclassified", provider.captures, provider.cancels, err)
	}
}
