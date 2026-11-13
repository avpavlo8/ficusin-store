package migrate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestManualPricesMoveToEditorDraftsOnDatabase(t *testing.T) {
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
	raw, err := os.ReadFile("../../../timeweb/migrations/20261113_zzzzzzz_catalog_editor_price_publications.sql")
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := os.ReadFile("../../../timeweb/migrations/20261113_zzzzzzzz_remove_retired_price_columns.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"drafts", "no legacy", "attempted", "submitted", "queued", "receipt reference", "lease"} {
		t.Run(scenario, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			exec := func(sql string) {
				t.Helper()
				if _, err := tx.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			active := fmt.Sprintf("manual_prices_%d", time.Now().UnixNano())
			archive := active + "_archive"
			exec("CREATE SCHEMA " + active + "; SET LOCAL search_path=" + active)
			exec(`CREATE TABLE customers(id BIGINT PRIMARY KEY); INSERT INTO customers VALUES(1);
CREATE TABLE products(id BIGINT PRIMARY KEY); INSERT INTO products VALUES(1);
CREATE TABLE product_variants(id BIGINT PRIMARY KEY,product_id BIGINT REFERENCES products(id),saby_id TEXT); INSERT INTO product_variants VALUES(101,1,'777');
CREATE TABLE procurement_action_batches(id BIGINT PRIMARY KEY,procurement_order_id BIGINT,kind TEXT DEFAULT 'prices',source TEXT DEFAULT 'procurement',manual_variant_id BIGINT,manual_site_price_minor BIGINT,status TEXT DEFAULT 'draft',created_by BIGINT,approved_by BIGINT,approved_at TIMESTAMPTZ,created_at TIMESTAMPTZ DEFAULT NOW(),updated_at TIMESTAMPTZ DEFAULT NOW(),CONSTRAINT procurement_action_batches_source_check CHECK(source IN('manual','procurement')));
CREATE INDEX procurement_manual_price_batches_variant_idx ON procurement_action_batches(manual_variant_id);
CREATE TABLE procurement_action_items(id BIGINT PRIMARY KEY,batch_id BIGINT REFERENCES procurement_action_batches(id) ON DELETE CASCADE,channel TEXT,external_article TEXT,new_value NUMERIC(14,2),compare_at_value NUMERIC(14,2),old_value NUMERIC(14,2),status TEXT DEFAULT 'draft',attempts INT DEFAULT 0,last_attempt_at TIMESTAMPTZ,completed_at TIMESTAMPTZ,external_operation_id TEXT DEFAULT '',locked_until TIMESTAMPTZ,lock_owner TEXT DEFAULT '',approved_by BIGINT,payload JSONB DEFAULT '{}'::JSONB,created_at TIMESTAMPTZ DEFAULT NOW(),updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE return_intakes(id BIGINT,receipt_action_id BIGINT REFERENCES procurement_action_items(id));
CREATE TABLE marketplace_returns(id BIGINT,receipt_action_id BIGINT REFERENCES procurement_action_items(id));
INSERT INTO procurement_action_batches(id,source,manual_variant_id,manual_site_price_minor,created_by) VALUES(10,'manual',101,123456,1);
INSERT INTO procurement_action_items(id,batch_id,channel,external_article,new_value,compare_at_value,old_value,payload) VALUES(11,10,'wb','123',1.23,47.10,20,'{"original":true}'),(12,10,'ozon','offer-id',25.50,NULL,20,'{"original":true}'),(13,10,'saby_price','Xcode',100,NULL,20,'{"original":true}');
INSERT INTO procurement_action_batches(id,source,procurement_order_id) VALUES(20,'procurement',999);
INSERT INTO procurement_action_items(id,batch_id,channel,external_article,new_value,status,attempts,external_operation_id) VALUES(21,20,'wb','untouched',42,'queued',99,'existing-receipt');`)
			switch scenario {
			case "no legacy":
				exec("DELETE FROM procurement_action_items WHERE batch_id=10; DELETE FROM procurement_action_batches WHERE id=10")
			case "attempted":
				exec("UPDATE procurement_action_items SET attempts=1 WHERE id=11")
			case "submitted":
				exec("UPDATE procurement_action_items SET external_operation_id='remote-op' WHERE id=11")
			case "queued":
				exec("UPDATE procurement_action_items SET status='queued' WHERE id=11")
			case "receipt reference":
				exec("INSERT INTO return_intakes VALUES(1,11)")
			case "lease":
				exec("UPDATE procurement_action_items SET locked_until=NOW()+INTERVAL '1 minute' WHERE id=11")
			}
			exec("SAVEPOINT retirement")
			sql := strings.ReplaceAll(string(raw), "legacy_archive", archive)
			_, err = tx.Exec(ctx, sql)
			if scenario != "drafts" && scenario != "no legacy" {
				if err == nil {
					t.Fatal("unsafe manual state was retired")
				}
				exec("ROLLBACK TO SAVEPOINT retirement")
				var n int
				if err := tx.QueryRow(ctx, "SELECT count(*) FROM procurement_action_items WHERE batch_id=10").Scan(&n); err != nil || n != 3 {
					t.Fatalf("failed migration changed rows: %d %v", n, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var n int
			cleanupSQL := strings.ReplaceAll(string(cleanup), "legacy_archive", archive)
			if scenario == "no legacy" {
				exec(cleanupSQL)
				if err := tx.QueryRow(ctx, "SELECT (SELECT count(*) FROM catalog_editor_price_publications)+(SELECT count(*) FROM "+archive+".manual_price_items)+(SELECT count(*) FROM "+archive+".manual_price_batches)").Scan(&n); err != nil || n != 0 {
					t.Fatalf("empty legacy history gained rows: %d %v", n, err)
				}
				if err := tx.QueryRow(ctx, "SELECT count(*) FROM procurement_action_items WHERE id=21 AND status='queued' AND external_operation_id='existing-receipt' AND attempts=99").Scan(&n); err != nil || n != 1 {
					t.Fatalf("empty-history cleanup changed procurement: %d %v", n, err)
				}
				return
			}
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM catalog_editor_price_publications WHERE status='draft' AND execution_mode='disabled' AND approved_by IS NULL AND approved_at IS NULL AND attempts=0 AND expected_site_price_minor=123456 AND created_by=1 AND price_origin='formula' AND fingerprint=encode(sha256(convert_to(payload::TEXT,'UTF8')),'hex')").Scan(&n); err != nil || n != 3 {
				t.Fatalf("draft migration changed approval/fingerprint: %d %v", n, err)
			}
			for _, check := range []struct {
				id, price       int64
				channel, target string
			}{{11, 123, "wb", "123"}, {12, 2550, "ozon", "offer-id"}, {13, 10000, "saby", "Xcode"}} {
				var price int64
				var channel, target string
				if err := tx.QueryRow(ctx, "SELECT price_minor,channel,target_id FROM catalog_editor_price_publications WHERE legacy_proposal_id=$1", check.id).Scan(&price, &channel, &target); err != nil || price != check.price || channel != check.channel || target != check.target {
					t.Fatalf("price/target migration: %d %s %s %v", price, channel, target, err)
				}
			}
			var sabyID string
			if err := tx.QueryRow(ctx, "SELECT payload->>'sabyId' FROM catalog_editor_price_publications WHERE legacy_proposal_id=13").Scan(&sabyID); err != nil || sabyID != "777" {
				t.Fatalf("Saby catalogue identity lost: %q %v", sabyID, err)
			}
			var compare int64
			if err := tx.QueryRow(ctx, "SELECT compare_at_minor FROM catalog_editor_price_publications WHERE legacy_proposal_id=11").Scan(&compare); err != nil || compare != 4710 {
				t.Fatalf("lost compare price: %d %v", compare, err)
			}
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+".manual_price_items WHERE payload='{"+`"original":true`+"}'::jsonb AND batch_id=10").Scan(&n); err != nil || n != 3 {
				t.Fatalf("archive lost source rows: %d %v", n, err)
			}
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM procurement_action_items WHERE id=21 AND status='queued' AND external_operation_id='existing-receipt' AND attempts=99").Scan(&n); err != nil || n != 1 {
				t.Fatalf("ordinary procurement changed: %d %v", n, err)
			}
			exec("SAVEPOINT old_producer")
			if _, err := tx.Exec(ctx, "INSERT INTO procurement_action_batches(id,source,procurement_order_id) VALUES(99,'manual',999)"); err == nil {
				t.Fatal("cached old producer can create manual source")
			}
			exec("ROLLBACK TO SAVEPOINT old_producer")
			exec(`UPDATE catalog_editor_price_publications SET execution_mode='live',status='queued',claim_token='preserved-token',attempts=2,progress='{"stage":"price-verify","ambiguous":true}' WHERE legacy_proposal_id=11`)
			exec("SAVEPOINT second_claim")
			if _, err := tx.Exec(ctx, "INSERT INTO catalog_editor_price_publications(product_id,variant_id,channel,target_id,execution_mode,status,fingerprint,expected_site_price_minor,price_minor,payload) VALUES(1,101,'wb','123','live','needs_attention','different',123456,222,'{}')"); err == nil {
				t.Fatal("shared remote target has two busy price operations")
			}
			exec("ROLLBACK TO SAVEPOINT second_claim")
			snapshotSQL := `SELECT jsonb_build_object(
 'batches',(SELECT jsonb_agg(to_jsonb(b)-'source' ORDER BY id) FROM procurement_action_batches b),
 'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM procurement_action_items i),
 'prices',(SELECT jsonb_agg(to_jsonb(p)-'old_value_minor' ORDER BY id) FROM catalog_editor_price_publications p),
 'archiveBatches',(SELECT jsonb_agg(to_jsonb(b) ORDER BY id) FROM ` + archive + `.manual_price_batches b),
 'archiveItems',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM ` + archive + `.manual_price_items i))::text`
			var before, after string
			if err := tx.QueryRow(ctx, snapshotSQL).Scan(&before); err != nil {
				t.Fatal(err)
			}
			exec("SAVEPOINT unarchived_price")
			exec("UPDATE catalog_editor_price_publications SET old_value_minor=999999 WHERE legacy_proposal_id=11")
			if _, err := tx.Exec(ctx, cleanupSQL); err == nil {
				t.Fatal("cleanup discarded an unarchived historical price")
			}
			exec("ROLLBACK TO SAVEPOINT unarchived_price")
			exec(cleanupSQL)
			if err := tx.QueryRow(ctx, snapshotSQL).Scan(&after); err != nil || before != after {
				t.Fatalf("cleanup changed working or archived rows: %v", err)
			}
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND ((table_name='procurement_action_batches' AND column_name='source') OR(table_name='catalog_editor_price_publications' AND column_name='old_value_minor'))`, active).Scan(&n); err != nil || n != 0 {
				t.Fatalf("retired price columns remain: %d %v", n, err)
			}
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+".manual_price_items i JOIN "+archive+".manual_price_batches b ON b.id=i.batch_id WHERE i.old_value=20 AND b.source='manual'").Scan(&n); err != nil || n != 3 {
				t.Fatalf("cleanup lost original price provenance: %d %v", n, err)
			}
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM catalog_editor_price_publications WHERE legacy_proposal_id=11 AND status='queued' AND execution_mode='live' AND price_minor=123 AND fingerprint=encode(sha256(convert_to(payload::text,'UTF8')),'hex')").Scan(&n); err != nil || n != 1 {
				t.Fatalf("cleanup changed active publication: %d %v", n, err)
			}
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM procurement_action_items WHERE id=21 AND status='queued' AND external_operation_id='existing-receipt' AND attempts=99").Scan(&n); err != nil || n != 1 {
				t.Fatalf("cleanup changed procurement operation: %d %v", n, err)
			}
			exec("SAVEPOINT retired_producer")
			if _, err := tx.Exec(ctx, "INSERT INTO procurement_action_batches(id,source,procurement_order_id) VALUES(99,'manual',999)"); err == nil {
				t.Fatal("cleanup allowed a cached old producer")
			}
			exec("ROLLBACK TO SAVEPOINT retired_producer")
		})
	}
}
