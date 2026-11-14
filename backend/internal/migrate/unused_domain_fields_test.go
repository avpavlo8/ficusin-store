package migrate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUnusedDomainFieldsArchiveExactValuesOnDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	raw, e := os.ReadFile("../../../timeweb/migrations/20261113_zzzzzzzzzzzz_archive_unused_domain_fields.sql")
	if e != nil {
		t.Fatal(e)
	}
	type domain struct {
		table, archive, pk string
		retired, history   []string
	}
	domains := []domain{
		{"products", "product_retired_sync_fields", "product_id", []string{"saby_snapshot"}, []string{"created_at", "updated_at"}},
		{"product_variants", "variant_retired_price_sync_fields", "variant_id", []string{"price_override_minor", "price_override_until", "saby_snapshot"}, []string{"product_id", "created_at", "updated_at"}},
		{"organizations", "organization_retired_terms", "organization_id", []string{"wholesale_discount_bps", "payment_terms"}, []string{"created_at", "approved_at"}},
		{"marketplace_returns", "marketplace_return_retired_amounts", "marketplace_return_id", []string{"channel_withheld_rub", "channel_compensation_rub"}, []string{"created_at", "updated_at"}},
		{"payment_refunds", "refund_retired_receipt_fields", "refund_id", []string{"receipt_required", "receipt_confirmed_by"}, []string{"created_at", "updated_at"}},
	}
	jsonFields := func(alias string, fields []string) string {
		args := []string{}
		for _, field := range fields {
			args = append(args, "'"+field+"'", alias+"."+pgx.Identifier{field}.Sanitize())
		}
		return "jsonb_build_object(" + strings.Join(args, ",") + ")"
	}
	for _, scenario := range []string{"existing values", "empty source"} {
		t.Run(scenario, func(t *testing.T) {
			tx, e := pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			must := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			exec := func(sql string, args ...any) { t.Helper(); _, e := tx.Exec(ctx, sql, args...); must(e) }
			schema := fmt.Sprintf("unused_domain_%d", time.Now().UnixNano())
			archive := schema + "_archive"
			exec("CREATE SCHEMA " + schema + ";SET LOCAL search_path=" + schema)
			for _, d := range domains {
				id := pgx.Identifier{d.table}.Sanitize()
				exec("CREATE TABLE " + id + " (LIKE public." + id + " INCLUDING DEFAULTS INCLUDING GENERATED INCLUDING IDENTITY INCLUDING CONSTRAINTS INCLUDING INDEXES)")
			}
			// Reconstruct retired fields if the surrounding database already ran217.
			exec(`ALTER TABLE products ADD COLUMN IF NOT EXISTS saby_snapshot JSONB NOT NULL DEFAULT '{}';
ALTER TABLE product_variants ADD COLUMN IF NOT EXISTS price_override_minor BIGINT,ADD COLUMN IF NOT EXISTS price_override_until TIMESTAMPTZ,ADD COLUMN IF NOT EXISTS saby_snapshot JSONB NOT NULL DEFAULT '{}';
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS wholesale_discount_bps INTEGER NOT NULL DEFAULT 0,ADD COLUMN IF NOT EXISTS payment_terms TEXT NOT NULL DEFAULT 'invoice';
ALTER TABLE marketplace_returns ADD COLUMN IF NOT EXISTS channel_withheld_rub NUMERIC(14,2),ADD COLUMN IF NOT EXISTS channel_compensation_rub NUMERIC(14,2);
ALTER TABLE payment_refunds ADD COLUMN IF NOT EXISTS receipt_required BOOLEAN NOT NULL DEFAULT FALSE,ADD COLUMN IF NOT EXISTS receipt_confirmed_by BIGINT;
CREATE TABLE customers(id BIGINT PRIMARY KEY);INSERT INTO customers VALUES(77);
ALTER TABLE payment_refunds ADD FOREIGN KEY(receipt_confirmed_by) REFERENCES customers(id);
CREATE TEMP TABLE domain_before(name TEXT,id BIGINT,current_values JSONB,archived_values JSONB);
CREATE TABLE historical_json_audit(id BIGINT PRIMARY KEY,data JSONB);
INSERT INTO historical_json_audit VALUES(1,'{"product":{"saby_snapshot":{"old":"source"}},"returns":[{"channel_withheld_rub":123.45}],"receipt_required":true}');`)
			if scenario == "existing values" {
				exec(`INSERT INTO products(id,name,slug,saby_snapshot) VALUES(1,'Current product','current-product','{"source":"old","nested":{"precise":123456789.123456789,"null":null}}'),(2,'Empty snapshot','empty-snapshot','{}');
INSERT INTO product_variants(id,product_id,label,base_price_minor,price_override_minor,price_override_until,saby_snapshot) VALUES(1,1,'D12',149099,125037,TIMESTAMPTZ '2030-03-04 05:06:07+00','{"original":[1,"text",null]}'),(2,2,'D18',299099,NULL,NULL,'{}');
INSERT INTO organizations(id,name,inn,wholesale_discount_bps,payment_terms,approved_at) VALUES(1,'Historical terms','1234567890',1250,'deferred-30',TIMESTAMPTZ '2021-02-03 04:05:06+00'),(2,'Default terms','1234567891',0,'invoice',NULL);
INSERT INTO marketplace_returns(id,channel,canonical_variant_id,returned_at,channel_withheld_rub,channel_compensation_rub,receipt_status,financial_status) VALUES(1,'ozon',1,CURRENT_DATE,123.45,67.89,'posted','linked'),(2,'wb',2,CURRENT_DATE,NULL,NULL,'none','incomplete');
INSERT INTO payment_refunds(id,order_id,payment_id,idempotence_key,amount,provider_refund_id,status,receipt_status,receipt_required,receipt_confirmed_by) VALUES(1,100,200,'historical-refund-1',15.37,'vendor-refund-id','succeeded','succeeded',TRUE,77),(2,101,201,'historical-refund-2',22.01,'','pending','pending',FALSE,NULL);
CREATE TABLE current_links(product_id BIGINT REFERENCES products(id),variant_id BIGINT REFERENCES product_variants(id),organization_id BIGINT REFERENCES organizations(id),return_id BIGINT REFERENCES marketplace_returns(id),refund_id BIGINT REFERENCES payment_refunds(id));
INSERT INTO current_links VALUES(1,1,1,1,1);`)
			}
			for _, d := range domains {
				fields := append(append([]string{}, d.retired...), d.history...)
				exec("INSERT INTO domain_before SELECT $1,id,to_jsonb(t)-$2::text[],"+jsonFields("t", fields)+" FROM "+d.table+" t", d.table, d.retired)
			}
			var auditBefore []byte
			must(tx.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM historical_json_audit a`).Scan(&auditBefore))
			exec(strings.ReplaceAll(string(raw), "legacy_archive", archive))
			for _, d := range domains {
				var n int
				must(tx.QueryRow(ctx, "SELECT count(*) FROM "+d.table+" t FULL JOIN (SELECT * FROM domain_before WHERE name=$1) b ON t.id=b.id WHERE t.id IS NULL OR b.id IS NULL OR to_jsonb(t) IS DISTINCT FROM b.current_values", d.table).Scan(&n))
				if n != 0 {
					t.Fatalf("%s current data changed", d.table)
				}
				fields := append(append([]string{}, d.retired...), d.history...)
				must(tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+"."+d.archive+" a FULL JOIN (SELECT * FROM domain_before WHERE name=$1) b ON a."+d.pk+"=b.id WHERE a."+d.pk+" IS NULL OR b.id IS NULL OR "+jsonFields("a", fields)+" IS DISTINCT FROM b.archived_values", d.table).Scan(&n))
				if n != 0 {
					t.Fatalf("%s archived values changed", d.table)
				}
				must(tx.QueryRow(ctx, "SELECT count(*) FROM pg_constraint WHERE conrelid=$1::regclass AND contype='f'", archive+"."+d.archive).Scan(&n))
				if n != 0 {
					t.Fatal("historical archive retains an active foreign key")
				}
				must(tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name=$2 AND column_name=ANY($3::text[])`, schema, d.table, d.retired).Scan(&n))
				if n != 0 {
					t.Fatalf("%s unused fields remain", d.table)
				}
				if scenario == "existing values" {
					exec("SAVEPOINT duplicate_archive")
					_, e := tx.Exec(ctx, "INSERT INTO "+archive+"."+d.archive+" SELECT * FROM "+archive+"."+d.archive+" LIMIT 1")
					pgErr, ok := e.(*pgconn.PgError)
					if !ok || pgErr.Code != "23505" {
						t.Fatalf("%s archive has no unique identity: %v", d.table, e)
					}
					exec("ROLLBACK TO SAVEPOINT duplicate_archive")
				}
			}
			exec(`DELETE FROM customers WHERE id=77`)
			if scenario == "existing values" {
				var author int64
				must(tx.QueryRow(ctx, "SELECT receipt_confirmed_by FROM "+archive+".refund_retired_receipt_fields WHERE refund_id=1").Scan(&author))
				if author != 77 {
					t.Fatal("retirement lost historical confirming author")
				}
			}
			var auditAfter []byte
			must(tx.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM historical_json_audit a`).Scan(&auditAfter))
			if string(auditBefore) != string(auditAfter) {
				t.Fatal("existing JSON audit was rewritten")
			}
			// New current rows need none of the retired fields. Their existence
			// must not synthesize historical values into the one-off archive.
			exec(`INSERT INTO products(id,name,slug) VALUES(3,'New current product','new-current-product');
INSERT INTO product_variants(id,product_id,label,base_price_minor) VALUES(3,3,'D20',99900);
INSERT INTO organizations(id,name,inn) VALUES(3,'New organization','1234567892');
INSERT INTO marketplace_returns(id,channel,source_return_id,canonical_variant_id,returned_at) VALUES(3,'wb','new-return-3',3,CURRENT_DATE);
INSERT INTO payment_refunds(id,order_id,payment_id,idempotence_key,amount) VALUES(3,102,202,'new-refund-3',9.99);`)
			for _, d := range domains {
				var n int
				must(tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+"."+d.archive+" WHERE "+d.pk+"=3").Scan(&n))
				if n != 0 {
					t.Fatal("new current row mutated historical archive")
				}
			}
		})
	}
}
