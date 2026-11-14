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

func TestLegacyOrderPaidAtPreservesPaymentHistoryOnLiveDatabase(t *testing.T) {
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
	raw, err := os.ReadFile("../../../timeweb/migrations/20261114_zzz_archive_legacy_order_paid_at.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"timestamps and NULL", "empty source", "dependent view"} {
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
			schema := fmt.Sprintf("order_paid_at_retirement_%d", time.Now().UnixNano())
			archive := schema + "_archive"
			exec("CREATE SCHEMA " + schema + "; SET LOCAL search_path=" + schema)
			for _, table := range []string{"orders", "payments"} {
				exec("CREATE TABLE " + table + " (LIKE public." + table + " INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)")
			}
			// This fixture works before and after the surrounding database has
			// retired the field. No public order or payment is inspected.
			exec(`ALTER TABLE orders ADD COLUMN IF NOT EXISTS paid_at TIMESTAMPTZ;
ALTER TABLE payments ADD FOREIGN KEY(order_id) REFERENCES orders(id);`)
			if scenario != "empty source" {
				exec(`INSERT INTO orders(id,order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status,payment_status,created_at,paid_at)
VALUES(101,'HISTORICAL-PAID','Payment history','+70000000000','','pickup',0,123.45,123.45,'confirmed','paid',TIMESTAMPTZ '2020-01-02 03:04:05.123456+03',TIMESTAMPTZ '2021-02-03 04:05:06.654321-04'),
(102,'HISTORICAL-NULL','Payment history','+70000000000','','pickup',0,99.99,99.99,'new','pending',TIMESTAMPTZ '2022-03-04 05:06:07.123456+00',NULL);
INSERT INTO payments(id,order_id,idempotence_key,provider_payment_id,amount,status,paid_at)
VALUES(201,101,'OWN-PAID-RECEIPT','own-provider-receipt',123.45,'paid',TIMESTAMPTZ '2021-02-03 04:05:06.111111+00'),
(202,102,'OWN-PENDING-RECEIPT','own-pending-receipt',99.99,'pending',NULL);`)
			}
			exec(`CREATE TEMP TABLE original_orders AS SELECT id,to_jsonb(o) AS value FROM orders o;
CREATE TEMP TABLE original_payments AS SELECT id,to_jsonb(p) AS value FROM payments p;`)
			if scenario == "dependent view" {
				exec("CREATE VIEW unexpected_paid_at_reader AS SELECT id,paid_at FROM orders;SAVEPOINT retire_paid_at")
			}
			sql := strings.ReplaceAll(strings.ReplaceAll(string(raw), "public.", schema+"."), "legacy_archive", archive)
			_, err = tx.Exec(ctx, sql)
			if scenario == "dependent view" {
				pgerr, ok := err.(*pgconn.PgError)
				if !ok || pgerr.Code != "2BP01" {
					t.Fatalf("dependent reader did not block DROP without CASCADE: %v", err)
				}
				exec("ROLLBACK TO SAVEPOINT retire_paid_at")
				var exists bool
				must(tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", archive+".order_retired_payment_timestamps").Scan(&exists))
				if exists {
					t.Fatal("blocked migration retained a partial archive")
				}
				var changed int
				must(tx.QueryRow(ctx, `SELECT count(*) FROM orders o FULL JOIN original_orders b USING(id) WHERE o.id IS NULL OR b.id IS NULL OR to_jsonb(o) IS DISTINCT FROM b.value`).Scan(&changed))
				if changed != 0 {
					t.Fatal("blocked migration changed orders")
				}
				exec("SELECT paid_at FROM unexpected_paid_at_reader")
			} else {
				must(err)
				var changed int
				must(tx.QueryRow(ctx, `SELECT count(*) FROM orders o FULL JOIN original_orders b USING(id) WHERE o.id IS NULL OR b.id IS NULL OR to_jsonb(o) IS DISTINCT FROM b.value-'paid_at'`).Scan(&changed))
				if changed != 0 {
					t.Fatal("retirement changed active order values")
				}
				must(tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+`.order_retired_payment_timestamps a FULL JOIN original_orders b ON a.order_id=b.id WHERE a.order_id IS NULL OR b.id IS NULL OR a.paid_at IS DISTINCT FROM (b.value->>'paid_at')::TIMESTAMPTZ OR a.original_created_at IS DISTINCT FROM (b.value->>'created_at')::TIMESTAMPTZ OR a.archived_at IS NULL`).Scan(&changed))
				if changed != 0 {
					t.Fatal("archive lost exact timestamps, NULLs or order IDs")
				}
				must(tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name='orders' AND column_name='paid_at'`, schema).Scan(&changed))
				if changed != 0 {
					t.Fatal("unused orders.paid_at remains")
				}
				must(tx.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid=$1::regclass AND contype='f'`, archive+".order_retired_payment_timestamps").Scan(&changed))
				if changed != 0 {
					t.Fatal("archive is coupled to active rows")
				}
			}
			var changed int
			must(tx.QueryRow(ctx, `SELECT count(*) FROM payments p FULL JOIN original_payments b USING(id) WHERE p.id IS NULL OR b.id IS NULL OR to_jsonb(p) IS DISTINCT FROM b.value`).Scan(&changed))
			if changed != 0 {
				t.Fatal("actual payment money, state or paid_at changed")
			}
			if scenario != "dependent view" {
				// The archive outlives active rows without restricting new writes.
				exec(`INSERT INTO orders(id,order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total) VALUES(103,'CURRENT-ORDER','Current payment','+70000000000','','pickup',0,10,10);
INSERT INTO payments(id,order_id,idempotence_key,amount,status,paid_at) VALUES(203,103,'CURRENT-PAYMENT',10,'paid',TIMESTAMPTZ '2026-01-02 03:04:05+00');DELETE FROM payments;DELETE FROM orders;`)
				var count int
				must(tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+".order_retired_payment_timestamps").Scan(&count))
				want := 2
				if scenario == "empty source" {
					want = 0
				}
				if count != want {
					t.Fatalf("current writes/deletions changed historical timestamps: %d", count)
				}
			}
		})
	}
}
