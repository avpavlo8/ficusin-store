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

func TestUnusedProcurementSettingsArchivePreservesPendingHistoryOnDatabase(t *testing.T) {
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
	raw, err := os.ReadFile("../../../timeweb/migrations/20261114_archive_unused_procurement_settings.sql")
	if err != nil {
		t.Fatal(err)
	}
	retired := []string{"domestic_retail_multiplier", "international_cost_multiplier", "international_retail_multiplier", "retail_round_step", "avoid_round_hundreds"}
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
			schema := fmt.Sprintf("retired_procurement_%d", time.Now().UnixNano())
			archive := schema + "_archive"
			exec("CREATE SCHEMA " + schema + ";SET LOCAL search_path=" + schema)
			for _, table := range []string{"procurement_pricing_settings", "procurement_orders", "procurement_order_lines", "procurement_action_batches", "procurement_action_items"} {
				exec("CREATE TABLE " + table + "(LIKE public." + table + " INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)")
			}
			exec(`ALTER TABLE procurement_pricing_settings ADD COLUMN IF NOT EXISTS domestic_retail_multiplier NUMERIC(8,4) NOT NULL DEFAULT 1,
   ADD COLUMN IF NOT EXISTS international_cost_multiplier NUMERIC(8,4) NOT NULL DEFAULT 1,
   ADD COLUMN IF NOT EXISTS international_retail_multiplier NUMERIC(8,4) NOT NULL DEFAULT 1,
   ADD COLUMN IF NOT EXISTS retail_round_step INTEGER NOT NULL DEFAULT 1,
   ADD COLUMN IF NOT EXISTS avoid_round_hundreds BOOLEAN NOT NULL DEFAULT FALSE;
   CREATE TABLE authors(id BIGINT PRIMARY KEY);INSERT INTO authors VALUES(77);
   ALTER TABLE procurement_pricing_settings ADD FOREIGN KEY(updated_by) REFERENCES authors(id);`)
			if scenario != "empty" {
				exec(`INSERT INTO procurement_pricing_settings(id,version,domestic_retail_multiplier,international_cost_multiplier,international_retail_multiplier,retail_round_step,avoid_round_hundreds,updated_by,updated_at)
    VALUES(1,37,2.1234,3.4567,4.9876,73,TRUE,77,TIMESTAMPTZ '2021-02-03 04:05:06+00');
    INSERT INTO procurement_orders(id,supplier_id,currency,status,calculation_version,calculation_settings)
    VALUES(1,100,'EUR','received',37,'{"domesticRetailMultiplier":2.1234,"internationalCostMultiplier":3.4567,"internationalRetailMultiplier":4.9876,"retailRoundStep":73,"avoidRoundHundreds":true,"unknown":{"exact":123456789.123456789,"null":null}}');
    INSERT INTO procurement_order_lines(id,procurement_order_id,raw_name,unit_cost_rub,channel_quotes)
    VALUES(1,1,'Historical invoice',123.45,'{"wb":{"priceRub":2490,"contributionRub":123.456789}}');
    INSERT INTO procurement_action_batches(id,procurement_order_id,kind,status,calculation_version,unified_price_snapshot)
    VALUES(1,1,'receipt','processing',37,'{"version":37,"items":[{"quote":{"priceRub":2490}}]}');
    INSERT INTO procurement_action_items(id,batch_id,channel,new_value,status,attempts,external_operation_id,payload)
    VALUES(1,1,'saby_receipt',123.45,'processing',3,'unknown-receipt-id','{"operationId":"unknown-receipt-id","price":123.450000,"oldSettings":{"retailRoundStep":73}}');`)
			}
			var currentBefore, archiveBefore []byte
			must(tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(s)-$1::text[] ORDER BY id),'[]'::jsonb) FROM procurement_pricing_settings s`, retired).Scan(&currentBefore))
			must(tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('settings_id',id,'version',version,'domestic_retail_multiplier',domestic_retail_multiplier,'international_cost_multiplier',international_cost_multiplier,'international_retail_multiplier',international_retail_multiplier,'retail_round_step',retail_round_step,'avoid_round_hundreds',avoid_round_hundreds,'updated_by',updated_by,'updated_at',updated_at) ORDER BY id),'[]'::jsonb) FROM procurement_pricing_settings`).Scan(&archiveBefore))
			history := map[string][]byte{}
			for _, table := range []string{"procurement_orders", "procurement_order_lines", "procurement_action_batches", "procurement_action_items"} {
				var before []byte
				must(tx.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM "+table+" t").Scan(&before))
				history[table] = before
			}
			exec("SAVEPOINT retirement")
			if scenario == "dependent view" {
				exec("CREATE VIEW active_dependency AS SELECT retail_round_step FROM procurement_pricing_settings")
			}
			_, err = tx.Exec(ctx, strings.ReplaceAll(string(raw), "legacy_archive", archive))
			if scenario == "dependent view" {
				if err == nil {
					t.Fatal("retirement silently removed a dependent object")
				}
				exec("ROLLBACK TO SAVEPOINT retirement")
				var n int
				must(tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name='procurement_pricing_settings' AND column_name=ANY($2::text[])`, schema, retired).Scan(&n))
				if n != 5 {
					t.Fatal("failed retirement did not restore source fields")
				}
				var exists bool
				must(tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", archive+".procurement_retired_pricing_settings").Scan(&exists))
				if exists {
					t.Fatal("failed retirement retained partial archive")
				}
				return
			}
			must(err)
			var currentAfter, archiveAfter []byte
			must(tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY id),'[]'::jsonb) FROM procurement_pricing_settings s`).Scan(&currentAfter))
			must(tx.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(a)-'archived_at' ORDER BY settings_id),'[]'::jsonb) FROM "+archive+".procurement_retired_pricing_settings a").Scan(&archiveAfter))
			if string(currentBefore) != string(currentAfter) {
				t.Fatalf("current settings/version changed: %s -> %s", currentBefore, currentAfter)
			}
			if string(archiveBefore) != string(archiveAfter) {
				t.Fatalf("archive lost exact values: %s -> %s", archiveBefore, archiveAfter)
			}
			var n int
			must(tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name='procurement_pricing_settings' AND column_name=ANY($2::text[])`, schema, retired).Scan(&n))
			if n != 0 {
				t.Fatal("retired columns remain")
			}
			must(tx.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid=$1::regclass AND contype='f'`, archive+".procurement_retired_pricing_settings").Scan(&n))
			if n != 0 {
				t.Fatal("archive still depends on live records")
			}
			for table, before := range history {
				var after []byte
				must(tx.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM "+table+" t").Scan(&after))
				if string(before) != string(after) {
					t.Fatalf("%s historical snapshot/receipt changed", table)
				}
			}
			exec("UPDATE procurement_pricing_settings SET updated_by=NULL;DELETE FROM authors WHERE id=77")
			if scenario == "existing" {
				var author int64
				must(tx.QueryRow(ctx, "SELECT updated_by FROM "+archive+".procurement_retired_pricing_settings WHERE settings_id=1").Scan(&author))
				if author != 77 {
					t.Fatal("archive lost historical author")
				}
			}
		})
	}
}
