package payment

import (
	"context"
	"fmt"
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
	service := &Service{pool: pool}
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
}
