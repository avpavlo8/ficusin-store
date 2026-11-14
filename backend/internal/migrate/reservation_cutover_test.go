package migrate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLegacyReservationCutoverOnLiveDatabase(t *testing.T) {
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
	raw, err := os.ReadFile("../../../timeweb/migrations/20261114_zzzzz_legacy_reservation_cutover.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"zero state", "inventory ownership", "order ownership", "offer ownership", "offer allocation"} {
		t.Run(scenario, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			exec := func(sql string, args ...any) { t.Helper(); _, e := tx.Exec(ctx, sql, args...); must(e) }
			schema := fmt.Sprintf("reservation_cutover_%d", time.Now().UnixNano())
			archive := schema + "_archive"
			exec("CREATE SCHEMA " + schema + ";SET LOCAL search_path=" + schema)
			tables := []string{"orders", "inventory", "order_items", "shipment_offer_items", "shipment_offer_inventory_reservations"}
			for _, table := range tables {
				exec("CREATE TABLE " + table + " (LIKE public." + table + " INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)")
			}
			// A historical fixture must be able to represent ownership that the
			// current public schema now forbids. Only private copies are relaxed.
			exec(`ALTER TABLE inventory DROP CONSTRAINT IF EXISTS inventory_legacy_reservation_zero;
ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_legacy_reservation_zero;
ALTER TABLE shipment_offer_items DROP CONSTRAINT IF EXISTS shipment_offer_items_legacy_reservation_zero;
ALTER TABLE shipment_offer_inventory_reservations DROP CONSTRAINT IF EXISTS shipment_offer_inventory_reservations_retired;
INSERT INTO orders(id,order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status,created_at,stock_released_at)
VALUES(1,'HISTORICAL-RELEASE','Fixture','+70000000000','','pickup',0,100,100,'cancelled',TIMESTAMPTZ '2020-01-02 03:04:05.123456+03',TIMESTAMPTZ '2021-02-03 04:05:06.654321-04'),
(2,'CURRENT-NO-RESERVE','Fixture','+70000000000','','pickup',0,100,100,'new',TIMESTAMPTZ '2022-03-04 05:06:07.123456+00',NULL);
INSERT INTO inventory(id,warehouse_id,variant_id,available_qty,reserved_qty,synced_at) VALUES(1,1,1,7,0,TIMESTAMPTZ '2026-01-01 01:02:03+00');
INSERT INTO order_items(id,order_id,product_id,variant_id,sku,product_name,quantity,unit_price,reserved_qty) VALUES(1,1,1,1,'900000000000000001','Own item',5,20,0);
INSERT INTO shipment_offer_items(id,shipment_offer_id,order_item_id,variant_id,product_name,sku,quantity,unit_price,checkout_reserved_qty) VALUES(1,1,1,1,'Own item','900000000000000001',1,20,0);`)
			switch scenario {
			case "inventory ownership":
				exec("UPDATE inventory SET reserved_qty=1")
			case "order ownership":
				exec("UPDATE order_items SET reserved_qty=1")
			case "offer ownership":
				exec("UPDATE shipment_offer_items SET checkout_reserved_qty=1")
			case "offer allocation":
				exec("INSERT INTO shipment_offer_inventory_reservations(shipment_offer_item_id,inventory_id,quantity) VALUES(1,1,1)")
			}
			before := map[string][]byte{}
			for _, table := range tables {
				var snapshot []byte
				must(tx.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM "+table+" t").Scan(&snapshot))
				before[table] = snapshot
			}
			exec("SAVEPOINT cutover")
			sql := strings.ReplaceAll(strings.ReplaceAll(string(raw), "public.", schema+"."), "legacy_archive", archive)
			_, err = tx.Exec(ctx, sql)
			if scenario != "zero state" {
				p, ok := err.(*pgconn.PgError)
				if !ok || p.Code != "P0001" {
					t.Fatalf("nonzero ownership did not block before cutover: %v", err)
				}
				exec("ROLLBACK TO SAVEPOINT cutover")
				var exists bool
				must(tx.QueryRow(ctx, "SELECT to_regnamespace($1) IS NOT NULL", archive).Scan(&exists))
				if exists {
					t.Fatal("blocked cutover left partial archives")
				}
			} else {
				must(err)
			}
			for _, table := range tables {
				var after []byte
				must(tx.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM "+table+" t").Scan(&after))
				if string(before[table]) != string(after) {
					t.Fatalf("cutover changed physical/current %s", table)
				}
			}
			if scenario != "zero state" {
				return
			}
			var changed int
			must(tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+`.reservation_cutover_orders a FULL JOIN orders o ON a.order_id=o.id WHERE a.order_id IS NULL OR o.id IS NULL OR a.stock_released_at IS DISTINCT FROM o.stock_released_at OR a.original_created_at IS DISTINCT FROM o.created_at OR a.archived_at IS NULL`).Scan(&changed))
			if changed != 0 {
				t.Fatal("order release timestamp/NULL history lost")
			}
			for _, entry := range []struct{ name, key, source, fields string }{
				{"reservation_cutover_inventory", "inventory_id", "inventory", "warehouse_id,variant_id,available_qty,reserved_qty,synced_at,stock_observed_at"},
				{"reservation_cutover_order_items", "order_item_id", "order_items", "order_id,variant_id,quantity,reserved_qty"},
				{"reservation_cutover_offer_items", "offer_item_id", "shipment_offer_items", "shipment_offer_id,order_item_id,variant_id,checkout_reserved_qty"},
			} {
				conditions := ""
				for _, field := range strings.Split(entry.fields, ",") {
					conditions += " OR a." + field + " IS DISTINCT FROM s." + field
				}
				must(tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+"."+entry.name+" a FULL JOIN "+entry.source+" s ON a."+entry.key+"=s.id WHERE a."+entry.key+" IS NULL OR s.id IS NULL"+conditions).Scan(&changed))
				if changed != 0 {
					t.Fatalf("archive lost exact %s attribution", entry.source)
				}
			}
			for _, statement := range []string{"UPDATE inventory SET reserved_qty=1", "UPDATE order_items SET reserved_qty=1", "UPDATE shipment_offer_items SET checkout_reserved_qty=1", "INSERT INTO shipment_offer_inventory_reservations(shipment_offer_item_id,inventory_id,quantity) VALUES(1,1,1)"} {
				exec("SAVEPOINT obsolete_producer")
				_, err = tx.Exec(ctx, statement)
				p, ok := err.(*pgconn.PgError)
				if !ok || p.Code != "23514" {
					t.Fatalf("cached positive writer accepted: %s %v", statement, err)
				}
				exec("ROLLBACK TO SAVEPOINT obsolete_producer")
			}
			exec("SAVEPOINT obsolete_release")
			_, err = tx.Exec(ctx, "UPDATE orders SET stock_released_at=NOW() WHERE id=2")
			p, ok := err.(*pgconn.PgError)
			if !ok || p.Code != "P0001" {
				t.Fatalf("cached release flag writer accepted: %v", err)
			}
			exec("ROLLBACK TO SAVEPOINT obsolete_release")
			exec("UPDATE orders SET status='cancelled' WHERE id=2;UPDATE inventory SET available_qty=8,stock_observed_at=NOW();DELETE FROM order_items;DELETE FROM shipment_offer_items;DELETE FROM orders;DELETE FROM inventory")
			must(tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+".reservation_cutover_orders").Scan(&changed))
			if changed != 2 {
				t.Fatal("current deletion erased detached history")
			}
			must(tx.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE connamespace=$1::regnamespace AND contype='f'`, archive).Scan(&changed))
			if changed != 0 {
				t.Fatal("archives depend on mutable live rows")
			}
		})
	}
}
