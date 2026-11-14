package order

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercise the real batch selection and stock-hold queries. A pending commercial
// revision must wait, rather than register a stale parcel or classify the order
// as permanently short of stock. No carrier request is permitted in this test.
func TestPendingRevisionShippingPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Path != "/ficusin_order_sale_test" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		t.Fatal("requires loopback dedicated ficusin_order_sale_test")
	}
	ctx := context.Background()
	control, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(ctx)
	schema := pgx.Identifier{fmt.Sprintf("sale_revision_shipping_%d", time.Now().UnixNano())}.Sanitize()
	if _, err = control.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer control.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = strings.Trim(schema, `"`)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(q string) {
		t.Helper()
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE test_pending(order_id BIGINT PRIMARY KEY);
 CREATE FUNCTION order_sale_revision_pending(BIGINT)RETURNS BOOLEAN LANGUAGE SQL STABLE AS 'SELECT EXISTS(SELECT 1 FROM test_pending WHERE order_id=$1)';
 CREATE FUNCTION order_sale_owned_quantity(BIGINT,BIGINT,BIGINT)RETURNS INTEGER LANGUAGE SQL STABLE AS 'SELECT 0';
 CREATE TABLE warehouses(id BIGINT PRIMARY KEY,saby_id TEXT,is_active INTEGER);
 CREATE TABLE products(id BIGINT PRIMARY KEY,saby_fields TEXT[]);
 CREATE TABLE product_variants(id BIGINT PRIMARY KEY,product_id BIGINT,saby_id TEXT);
 CREATE TABLE saby_nomenclature(saby_id TEXT,missing_since TIMESTAMPTZ);
 CREATE TABLE order_sale_inventory(warehouse_id BIGINT,variant_id BIGINT,available_qty INTEGER,reserved_qty INTEGER,synced_at TIMESTAMPTZ);
 CREATE TABLE orders(id BIGINT PRIMARY KEY,order_number TEXT,customer_name TEXT DEFAULT 'Synthetic',phone TEXT DEFAULT '',
 delivery_method TEXT DEFAULT 'cdek',delivery_payee TEXT DEFAULT 'shop',cdek_uuid TEXT DEFAULT '',cdek_create_state TEXT DEFAULT 'pending',
 cdek_last_error TEXT DEFAULT '',cdek_next_attempt_at TIMESTAMPTZ,cdek_attempts INTEGER DEFAULT 0,cdek_order_number TEXT DEFAULT '',
 cdek_office_code TEXT DEFAULT 'test',cdek_tariff_code INTEGER DEFAULT 136,cdek_city_code INTEGER DEFAULT 1,
 delivery_fee_pending INTEGER DEFAULT 0,has_preorder INTEGER DEFAULT 0,status TEXT DEFAULT 'ready',payment_status TEXT DEFAULT 'paid',
 cdek_checkout_requested INTEGER DEFAULT 0,total NUMERIC DEFAULT 100,delivery_fee NUMERIC DEFAULT 0);
 CREATE TABLE order_items(order_id BIGINT,variant_id BIGINT,quantity INTEGER);
 CREATE TABLE shipment_offers(id BIGINT PRIMARY KEY,order_id BIGINT,status TEXT DEFAULT 'paid',delivery_method TEXT DEFAULT 'cdek',
 delivery_payee TEXT DEFAULT 'shop',cdek_uuid TEXT DEFAULT '',cdek_create_state TEXT DEFAULT 'new',cdek_last_error TEXT DEFAULT '',
 cdek_next_attempt_at TIMESTAMPTZ,cdek_attempts INTEGER DEFAULT 0,cdek_tariff_code INTEGER DEFAULT 136,updated_at TIMESTAMPTZ);
 CREATE TABLE shipment_offer_items(shipment_offer_id BIGINT,variant_id BIGINT,quantity INTEGER);
 INSERT INTO warehouses VALUES(1,'saby-ryazan-main',1);
 INSERT INTO products VALUES(1,ARRAY['stock']);INSERT INTO product_variants VALUES(1,1,'3980');INSERT INTO saby_nomenclature VALUES('3980',NULL);
 INSERT INTO order_sale_inventory VALUES(1,1,0,0,now());
 INSERT INTO orders(id,order_number)VALUES(1,'REVISION-MAIN'),(2,'REVISION-OFFER');
 INSERT INTO order_items VALUES(1,1,1),(2,1,1);
 INSERT INTO shipment_offers(id,order_id)VALUES(21,2);INSERT INTO shipment_offer_items VALUES(21,1,1);
 INSERT INTO test_pending VALUES(1),(2);`)
	var logs bytes.Buffer
	worker := &ShippingWorker{pool: pool, cdek: &realizationReadOnlyCarrier{}, logger: slog.New(slog.NewTextHandler(&logs, nil))}
	worker.markStockShortages(ctx)
	// Restore stock to ensure the dispatch candidates would otherwise be eligible.
	exec(`UPDATE order_sale_inventory SET available_qty=10`)
	worker.createShipments(ctx)
	worker.createOfferShipments(ctx)
	if logs.Len() != 0 {
		t.Fatalf("batch query failed instead of waiting safely: %s", logs.String())
	}
	var state string
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT cdek_create_state,cdek_attempts FROM orders WHERE id=1`).Scan(&state, &attempts); err != nil || state != "pending" || attempts != 0 {
		t.Fatalf("pending main order was claimed or stock-blocked: %s/%d %v", state, attempts, err)
	}
	if err := pool.QueryRow(ctx, `SELECT cdek_create_state,cdek_attempts FROM shipment_offers WHERE id=21`).Scan(&state, &attempts); err != nil || state != "new" || attempts != 0 {
		t.Fatalf("pending partial offer was claimed or stock-blocked: %s/%d %v", state, attempts, err)
	}
	// Control: the same stock shortage is still surfaced when sync is settled.
	exec(`DELETE FROM test_pending;UPDATE order_sale_inventory SET available_qty=0`)
	worker.markStockShortages(ctx)
	if logs.Len() != 0 {
		t.Fatal(logs.String())
	}
	if err := pool.QueryRow(ctx, `SELECT cdek_create_state FROM orders WHERE id=1`).Scan(&state); err != nil || state != "manual_review" {
		t.Fatalf("settled main shortage disappeared: %s %v", state, err)
	}
	if err := pool.QueryRow(ctx, `SELECT cdek_create_state FROM shipment_offers WHERE id=21`).Scan(&state); err != nil || state != "manual_review" {
		t.Fatalf("settled offer shortage disappeared: %s %v", state, err)
	}
}
