package migrate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLegacyArchivePreservesDataAndActiveWorkflows(t *testing.T) {
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
	raw, err := os.ReadFile("../../../timeweb/migrations/20261113_zz_archive_unused_tables.sql")
	if err != nil {
		t.Fatal(err)
	}
	tables := []string{"finance_imports", "finance_transactions", "finance_classification_versions", "finance_cash_entries", "finance_cash_reconciliations", "finance_packaging_snapshots", "finance_marketplace_adjustments", "finance_tax_periods", "supplier_account_operations", "supplier_account_reconciliations", "finance_marketplace_periods", "finance_marketplace_report_rows", "finance_classification_rules", "product_creation_photos", "product_creation_staging_cards"}
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprintf("archive_collision_%t", collision), func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := tx.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			active := fmt.Sprintf("cleanup_active_%d", time.Now().UnixNano())
			archive := active + "_archive"
			exec("CREATE SCHEMA " + active + "; CREATE SCHEMA " + archive + "; SET LOCAL search_path=" + active)
			exec(`CREATE TABLE customers(id BIGINT PRIMARY KEY); INSERT INTO customers VALUES(1);
CREATE TABLE sales_events(id BIGINT PRIMARY KEY);
CREATE TABLE orders(id BIGINT PRIMARY KEY);
CREATE TABLE shipment_offers(id BIGINT PRIMARY KEY);`)
			original := []byte{0, 1, 2, 255}
			for _, table := range tables {
				exec("CREATE TABLE " + table + "(id BIGSERIAL PRIMARY KEY,payload BYTEA NOT NULL,customer_id BIGINT REFERENCES customers(id) ON DELETE CASCADE)")
				exec("INSERT INTO "+table+"(payload,customer_id) VALUES($1,1)", original)
			}
			exec("ALTER TABLE finance_transactions ADD COLUMN import_id BIGINT REFERENCES finance_imports(id); UPDATE finance_transactions SET import_id=1")
			for _, fn := range []string{"snapshot_finance_packaging", "recognize_site_order_packaging", "recognize_site_offer_packaging", "stamp_actual_shipment_time"} {
				exec("CREATE FUNCTION " + fn + "() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$")
			}
			exec("CREATE FUNCTION finance_packaging_eligible(sales_events) RETURNS bool LANGUAGE sql AS $$ SELECT true $$")
			exec(`CREATE TRIGGER sales_events_finance_packaging AFTER INSERT ON sales_events FOR EACH ROW EXECUTE FUNCTION snapshot_finance_packaging();
CREATE TRIGGER orders_finance_packaging_recognition AFTER INSERT ON orders FOR EACH ROW EXECUTE FUNCTION recognize_site_order_packaging();
CREATE TRIGGER shipment_offers_finance_packaging_recognition AFTER INSERT ON shipment_offers FOR EACH ROW EXECUTE FUNCTION recognize_site_offer_packaging();
CREATE TRIGGER orders_actual_shipment_time BEFORE INSERT ON orders FOR EACH ROW EXECUTE FUNCTION stamp_actual_shipment_time();`)
			sql := strings.ReplaceAll(strings.ReplaceAll(string(raw), "legacy_archive", archive), "public", active)
			if collision {
				exec("CREATE TABLE " + archive + ".finance_imports(id BIGINT)")
				exec("SAVEPOINT cleanup")
				if _, err := tx.Exec(ctx, sql); err == nil {
					t.Fatal("archive collision must fail rather than replace records")
				}
				exec("ROLLBACK TO SAVEPOINT cleanup")
				var count int
				if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM "+active+".finance_imports").Scan(&count); err != nil || count != 1 {
					t.Fatalf("failed migration changed source: %d %v", count, err)
				}
				if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM pg_trigger WHERE tgrelid=$1::regclass AND tgname='sales_events_finance_packaging'", active+".sales_events").Scan(&count); err != nil || count != 1 {
					t.Fatalf("failed migration removed active trigger: %d %v", count, err)
				}
				return
			}
			exec(sql)
			// Active customer deletion must not cascade into archived source bytes.
			exec("DELETE FROM customers WHERE id=1")
			for _, table := range tables {
				var sourceExists bool
				if err := tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", active+"."+table).Scan(&sourceExists); err != nil || sourceExists {
					t.Fatalf("active table remains: %s %v", table, err)
				}
				var got []byte
				if err := tx.QueryRow(ctx, "SELECT payload FROM "+archive+"."+table+" WHERE id=1").Scan(&got); err != nil || !bytes.Equal(got, original) {
					t.Fatalf("archive lost bytes: %s %x %v", table, got, err)
				}
			}
			// Owned sequence and archive-internal foreign keys must survive the move.
			var next int64
			if err := tx.QueryRow(ctx, "INSERT INTO "+archive+".finance_imports(payload) VALUES($1) RETURNING id", original).Scan(&next); err != nil || next != 2 {
				t.Fatalf("archive sequence: %d %v", next, err)
			}
			var fks, triggers int
			if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM pg_constraint WHERE conrelid=$1::regclass AND contype='f'", archive+".finance_transactions").Scan(&fks); err != nil || fks != 1 {
				t.Fatalf("archive internal FK: %d %v", fks, err)
			}
			if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM pg_trigger WHERE tgrelid=$1::regclass AND NOT tgisinternal", active+".orders").Scan(&triggers); err != nil || triggers != 1 {
				t.Fatalf("shipment timestamp trigger: %d %v", triggers, err)
			}
			exec("INSERT INTO orders VALUES(1); INSERT INTO shipment_offers VALUES(1); INSERT INTO sales_events VALUES(1)")
		})
	}
}
