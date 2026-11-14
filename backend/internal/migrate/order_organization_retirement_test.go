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

func TestRetiredOrderOrganizationLinksPreserveHistoryOnLiveDatabase(t *testing.T) {
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
	raw, err := os.ReadFile("../../../timeweb/migrations/20261114_zz_archive_unused_order_organization_links.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"existing links and NULL", "empty source", "dependent view"} {
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
			schema := fmt.Sprintf("order_org_retirement_%d", time.Now().UnixNano())
			archive := schema + "_archive"
			exec("CREATE SCHEMA " + schema + "; SET LOCAL search_path=" + schema)
			for _, table := range []string{"orders", "customers", "organizations", "organization_members"} {
				exec("CREATE TABLE " + table + " (LIKE public." + table + " INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)")
			}
			// CI applies all migrations first. Reconstruct the real retired nullable
			// FK in this private fixture, independently of the surrounding DB version.
			exec(`ALTER TABLE orders ADD COLUMN IF NOT EXISTS organization_id BIGINT;
ALTER TABLE orders ADD FOREIGN KEY(organization_id) REFERENCES organizations(id);
ALTER TABLE orders ADD FOREIGN KEY(customer_id) REFERENCES customers(id);
ALTER TABLE organization_members ADD FOREIGN KEY(organization_id) REFERENCES organizations(id) ON DELETE CASCADE;
ALTER TABLE organization_members ADD FOREIGN KEY(customer_id) REFERENCES customers(id) ON DELETE CASCADE;
INSERT INTO customers(id,phone,full_name,consent_at) VALUES(11,'+79990000011','Organization owner',CURRENT_TIMESTAMP);
INSERT INTO organizations(id,name,inn) VALUES(7,'Active organization','1234567890');
INSERT INTO organization_members(id,organization_id,customer_id,role) VALUES(17,7,11,'owner');`)
			if scenario != "empty source" {
				exec(`INSERT INTO orders(id,order_number,customer_id,organization_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,created_at,cdek_create_state,cdek_uuid,payment_status)
VALUES(101,'OLD-ORG-LINK',11,7,'Historical buyer','+79990000011','buyer@example.invalid','pickup',0,123.45,123.45,TIMESTAMPTZ '2021-02-03 04:05:06.123456+03','unknown','uncertain-native-shipment','payment_provider_pending'),
(102,'OLD-NULL-LINK',11,NULL,'Individual buyer','+79990000011','','pickup',0,12.34,12.34,TIMESTAMPTZ '2022-03-04 05:06:07.654321-04','pending','','paid');`)
			}
			exec(`CREATE TEMP TABLE original_orders AS SELECT id,to_jsonb(o) AS value FROM orders o;
CREATE TEMP TABLE original_members AS SELECT id,to_jsonb(m) AS value FROM organization_members m;`)
			if scenario == "dependent view" {
				exec("CREATE VIEW unexpected_order_link_reader AS SELECT id,organization_id FROM orders; SAVEPOINT retire_order_link")
			}
			sql := strings.ReplaceAll(strings.ReplaceAll(string(raw), "public.", schema+"."), "legacy_archive", archive)
			_, err = tx.Exec(ctx, sql)
			if scenario == "dependent view" {
				pgerr, ok := err.(*pgconn.PgError)
				if !ok || pgerr.Code != "2BP01" {
					t.Fatalf("dependent view did not block DROP without CASCADE: %v", err)
				}
				exec("ROLLBACK TO SAVEPOINT retire_order_link")
				var archiveExists bool
				must(tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", archive+".order_retired_organization_links").Scan(&archiveExists))
				if archiveExists {
					t.Fatal("failed migration left a partial archive")
				}
				var changed int
				must(tx.QueryRow(ctx, `SELECT count(*) FROM orders o FULL JOIN original_orders b ON o.id=b.id WHERE o.id IS NULL OR b.id IS NULL OR to_jsonb(o) IS DISTINCT FROM b.value`).Scan(&changed))
				if changed != 0 {
					t.Fatal("blocked migration changed original orders")
				}
				exec("SELECT organization_id FROM unexpected_order_link_reader")
				return
			}
			must(err)
			var changed int
			must(tx.QueryRow(ctx, `SELECT count(*) FROM orders o FULL JOIN original_orders b ON o.id=b.id WHERE o.id IS NULL OR b.id IS NULL OR to_jsonb(o) IS DISTINCT FROM b.value-'organization_id'`).Scan(&changed))
			if changed != 0 {
				t.Fatal("retirement changed current order values, payment or shipping state")
			}
			must(tx.QueryRow(ctx, `SELECT count(*) FROM organization_members m FULL JOIN original_members b ON m.id=b.id WHERE m.id IS NULL OR b.id IS NULL OR to_jsonb(m) IS DISTINCT FROM b.value`).Scan(&changed))
			if changed != 0 {
				t.Fatal("retirement changed active organization membership")
			}
			must(tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+`.order_retired_organization_links a FULL JOIN original_orders b ON a.order_id=b.id WHERE a.order_id IS NULL OR b.id IS NULL OR a.organization_id IS DISTINCT FROM (b.value->>'organization_id')::BIGINT OR a.original_created_at IS DISTINCT FROM (b.value->>'created_at')::TIMESTAMPTZ OR a.archived_at IS NULL`).Scan(&changed))
			if changed != 0 {
				t.Fatal("archive lost exact order identity, NULL link or timestamp")
			}
			var columns int
			must(tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name='orders' AND column_name='organization_id'`, schema).Scan(&columns))
			if columns != 0 {
				t.Fatal("unused direct link remains")
			}
			must(tx.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid=$1::regclass AND contype='f'`, archive+".order_retired_organization_links").Scan(&columns))
			if columns != 0 {
				t.Fatal("archive has live foreign keys")
			}
			if scenario != "empty source" {
				exec("SAVEPOINT duplicate_history")
				_, err = tx.Exec(ctx, "INSERT INTO "+archive+".order_retired_organization_links SELECT * FROM "+archive+".order_retired_organization_links LIMIT 1")
				pgerr, ok := err.(*pgconn.PgError)
				if !ok || pgerr.Code != "23505" {
					t.Fatalf("archive has no unique order identity: %v", err)
				}
				exec("ROLLBACK TO SAVEPOINT duplicate_history")
			}
			// New checkout-shaped rows need only the current customer identity, and
			// organization registration/membership still works independently.
			exec(`INSERT INTO orders(id,order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total)
VALUES(103,'NEW-CUSTOMER-ORDER',11,'Current buyer','+79990000011','','pickup',0,99.99,99.99);
INSERT INTO organizations(id,name,inn) VALUES(8,'New organization','1234567891');
INSERT INTO organization_members(id,organization_id,customer_id,role) VALUES(18,8,11,'owner');`)
			var members int
			must(tx.QueryRow(ctx, `SELECT count(*) FROM organization_members WHERE customer_id=11 AND organization_id IN(7,8)`).Scan(&members))
			if members != 2 {
				t.Fatal("active registration membership failed")
			}
			exec("DELETE FROM orders; DELETE FROM organization_members WHERE organization_id=7; DELETE FROM organizations WHERE id=7")
			var historical int
			must(tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+".order_retired_organization_links").Scan(&historical))
			expected := 2
			if scenario == "empty source" {
				expected = 0
			}
			if historical != expected {
				t.Fatalf("new writes/deletions changed detached history: %d want %d", historical, expected)
			}
		})
	}
}
