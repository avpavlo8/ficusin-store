package migrate

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRetiredWorkflowTimestampsPreserveHistoryOnDatabase(t *testing.T) {
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
	raw, err := os.ReadFile("../../../timeweb/migrations/20261114_z_archive_retired_workflow_timestamps.sql")
	if err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile("../../../timeweb/migrations/20260919_crm_stage13_finance_completion.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Reinstall the actual historical function even after the surrounding DB
	// has already applied retirement. Do not replace it with a permissive stub.
	stamp := regexp.MustCompile(`(?s)CREATE OR REPLACE FUNCTION stamp_actual_shipment_time\(\).*?\$\$ LANGUAGE plpgsql;`).FindString(string(previous))
	if stamp == "" {
		t.Fatal("historical shipment timestamp function not found")
	}
	columns := map[string]string{"orders": "shipped_at", "shipment_offers": "shipped_at", "products": "saby_updated_at", "product_variants": "saby_updated_at"}
	for _, scenario := range []string{"existing", "empty", "dependent view"} {
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
			exec := func(sql string, args ...any) { t.Helper(); _, err := tx.Exec(ctx, sql, args...); must(err) }
			schema := fmt.Sprintf("retired_timestamps_%d", time.Now().UnixNano())
			archive := schema + "_archive"
			exec("CREATE SCHEMA " + schema + ";SET LOCAL search_path=" + schema)
			for table, column := range columns {
				exec("CREATE TABLE " + table + "(LIKE public." + table + " INCLUDING DEFAULTS INCLUDING CONSTRAINTS);ALTER TABLE " + table + " ADD COLUMN IF NOT EXISTS " + column + " TIMESTAMPTZ")
			}
			exec(stamp)
			exec(`CREATE TRIGGER orders_actual_shipment_time BEFORE UPDATE OF status ON orders FOR EACH ROW EXECUTE FUNCTION stamp_actual_shipment_time();
CREATE TRIGGER shipment_offers_actual_shipment_time BEFORE UPDATE OF status ON shipment_offers FOR EACH ROW EXECUTE FUNCTION stamp_actual_shipment_time();`)
			if scenario != "empty" {
				exec(`INSERT INTO orders(id,order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status,shipped_at,cdek_create_state,cdek_uuid)
 VALUES(1,'ARCHIVE-1','Customer','+79990000000','','pickup',0,123.45,123.45,'shipped',TIMESTAMPTZ '2021-02-03 04:05:06.123456+03','unknown','uncertain-shipment'),
 (2,'ARCHIVE-2','Customer','+79990000000','','pickup',0,0,0,'new',NULL,'pending','');
INSERT INTO shipment_offers(id,order_id,public_token,order_revision,delivery_method,status,shipped_at)
 VALUES(1,1,'historical-offer',1,'pickup','shipped',TIMESTAMPTZ '2022-03-04 05:06:07.654321-04'),(2,2,'pending-offer',1,'pickup','draft',NULL);
INSERT INTO products(id,name,slug,saby_updated_at) VALUES(1,'Historical product','historical-product',TIMESTAMPTZ '2020-01-02 03:04:05.123456+00'),(2,'New product','new-product',NULL);
INSERT INTO product_variants(id,product_id,label,base_price_minor,saby_updated_at) VALUES(1,1,'D12',149099,TIMESTAMPTZ '2023-04-05 06:07:08.987654+00'),(2,2,'D18',299099,NULL);`)
			}
			for table := range columns {
				exec("CREATE TEMP TABLE " + table + "_before AS SELECT id,to_jsonb(t) original FROM " + table + " t")
			}
			if scenario == "dependent view" {
				exec("CREATE VIEW shipment_timestamp_consumer AS SELECT shipped_at FROM orders")
			}
			exec("SAVEPOINT retirement")
			_, err = tx.Exec(ctx, strings.ReplaceAll(strings.ReplaceAll(string(raw), "public.", schema+"."), "legacy_archive", archive))
			if scenario == "dependent view" {
				if err == nil {
					t.Fatal("unexpected dependency was silently removed")
				}
				exec("ROLLBACK TO SAVEPOINT retirement")
				var count int
				must(tx.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid IN ($1::regclass,$2::regclass) AND tgname IN ('orders_actual_shipment_time','shipment_offers_actual_shipment_time')`, schema+".orders", schema+".shipment_offers").Scan(&count))
				if count != 2 {
					t.Fatal("failed migration did not restore triggers")
				}
				var absent bool
				must(tx.QueryRow(ctx, "SELECT to_regclass($1) IS NULL", archive+".retired_workflow_timestamps").Scan(&absent))
				if !absent {
					t.Fatal("failed migration retained a partial archive")
				}
			}
			if scenario != "dependent view" {
				must(err)
			}
			for table, column := range columns {
				var count int
				query := "SELECT count(*) FROM " + table + " t FULL JOIN " + table + "_before b USING(id) WHERE t.id IS NULL OR b.id IS NULL OR "
				if scenario == "dependent view" {
					query += "to_jsonb(t) IS DISTINCT FROM b.original"
				} else {
					query += "to_jsonb(t) IS DISTINCT FROM (b.original-$1::text)"
				}
				if scenario == "dependent view" {
					must(tx.QueryRow(ctx, query).Scan(&count))
				} else {
					must(tx.QueryRow(ctx, query, column).Scan(&count))
				}
				if count != 0 {
					t.Fatalf("%s current rows or unknown provider state changed", table)
				}
				if scenario == "dependent view" {
					continue
				}
				var expected, actual []byte
				must(tx.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(jsonb_build_object('source_table',$1::text,'source_id',id,'source_column',$2::text,'value',original->$2::text) ORDER BY id),'[]'::jsonb) FROM "+table+"_before", table, column).Scan(&expected))
				must(tx.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(a)-'archived_at' ORDER BY source_id),'[]'::jsonb) FROM "+archive+".retired_workflow_timestamps a WHERE source_table=$1", table).Scan(&actual))
				if string(expected) != string(actual) {
					t.Fatalf("%s archive lost exact timestamps/nulls: %s => %s", table, expected, actual)
				}
				must(tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name=$2 AND column_name=$3`, schema, table, column).Scan(&count))
				if count != 0 {
					t.Fatalf("%s.%s remains active", table, column)
				}
			}
			if scenario == "dependent view" {
				return
			}
			var count int
			must(tx.QueryRow(ctx, `SELECT count(*) FROM pg_proc WHERE pronamespace=$1::regnamespace AND proname='stamp_actual_shipment_time'`, schema).Scan(&count))
			if count != 0 {
				t.Fatal("retired function remains")
			}
			must(tx.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid=$1::regclass AND contype='f'`, archive+".retired_workflow_timestamps").Scan(&count))
			if count != 0 {
				t.Fatal("archive is coupled to active rows")
			}
			// Status transitions must work after removal; archive remains immutable.
			exec("UPDATE orders SET status='completed';UPDATE shipment_offers SET status='shipped';DELETE FROM product_variants;DELETE FROM products")
			must(tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+".retired_workflow_timestamps").Scan(&count))
			if scenario == "existing" && count != 8 {
				t.Fatal("active row deletion erased historical timestamps")
			}
		})
	}
}
