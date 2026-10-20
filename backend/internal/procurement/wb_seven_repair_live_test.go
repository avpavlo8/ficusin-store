package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWBSevenRepairPreservesHistoryAndRejectsConflicts(t *testing.T) {
	dsn := os.Getenv("CRM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repair, err := os.ReadFile("../../../timeweb/migrations/20261020_wb_seven_recreated_cards.sql")
	if err != nil {
		t.Fatal(err)
	}
	trigger, err := os.ReadFile("../../../timeweb/migrations/20261018_procurement_channel_variant_mapping.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"complete", "empty", "partial", "taken", "unexpected"} {
		t.Run(scenario, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx) //nolint:errcheck
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			schema := fmt.Sprintf("wb_repair_%d", time.Now().UnixNano())
			_, err = tx.Exec(ctx, "CREATE SCHEMA "+schema+"; SET LOCAL search_path TO "+schema)
			must(err)
			_, err = tx.Exec(ctx, `
CREATE TABLE canonical_product_directory(variant_id BIGINT,product_id BIGINT,internal_sku TEXT,master_code TEXT,saby_id TEXT,active BOOLEAN);
CREATE TABLE product_external_ids(id BIGSERIAL PRIMARY KEY,product_id BIGINT,variant_id BIGINT,provider TEXT,id_type TEXT,external_id TEXT,status TEXT,is_primary BOOLEAN,source TEXT,metadata JSONB DEFAULT '{}',last_seen_at TIMESTAMPTZ,updated_at TIMESTAMPTZ,UNIQUE(provider,id_type,external_id));
CREATE TABLE procurement_product_channels(saby_id TEXT PRIMARY KEY,wb_nm_id BIGINT,wb_vendor_code TEXT,ozon_offer_id TEXT DEFAULT '',updated_at TIMESTAMPTZ);
CREATE TABLE protected_business_data(kind TEXT PRIMARY KEY,value TEXT);
INSERT INTO protected_business_data VALUES('site-price','1490'),('stock','14'),('cogs','743.12'),('receipt','posted'),('external-queue','empty');
INSERT INTO canonical_product_directory VALUES
(1,1,'1','X1150532','s1',TRUE),(87,87,'87','X315119260','s87',TRUE),(145,145,'145','X933312684','s145',TRUE),(231,230,'231','X7774683','s231',TRUE),
(872,11407,'872','X615770892','s872',TRUE),(873,11408,'873','X525558640','s873',TRUE),(874,11409,'874','X740063870','s874',TRUE);
INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,status,is_primary,source) VALUES
(1,1,'wildberries','sku','1106114508','active',TRUE,'manual'),(87,87,'wildberries','sku','378558302','active',TRUE,'manual'),
(145,145,'wildberries','sku','262766221','active',TRUE,'manual'),(230,231,'wildberries','sku','964191777','active',TRUE,'manual'),
(11407,872,'ozon','offer_id','каламондин д9','active',TRUE,'manual');`)
			must(err)
			_, err = tx.Exec(ctx, string(trigger))
			must(err)
			_, err = tx.Exec(ctx, `CREATE TRIGGER sync_wb AFTER INSERT OR UPDATE ON procurement_product_channels FOR EACH ROW EXECUTE FUNCTION sync_marketplace_external_ids()`)
			must(err)
			switch scenario {
			case "empty":
				_, err = tx.Exec(ctx, "DELETE FROM canonical_product_directory")
			case "partial":
				_, err = tx.Exec(ctx, "DELETE FROM canonical_product_directory WHERE internal_sku='874'")
			case "taken":
				_, err = tx.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,status) VALUES(999,999,'wildberries','sku','1938138261','active')`)
			case "unexpected":
				_, err = tx.Exec(ctx, `UPDATE product_external_ids SET external_id='999999' WHERE external_id='262766221'`)
			}
			must(err)
			_, err = tx.Exec(ctx, string(repair))
			if scenario == "partial" || scenario == "taken" || scenario == "unexpected" {
				if err == nil {
					t.Fatal("unsafe repair was accepted")
				}
				return
			}
			must(err)
			var current, legacy, ozon, protected int
			must(tx.QueryRow(ctx, `SELECT COUNT(*) FILTER(WHERE provider='wildberries' AND id_type='sku' AND status='active'),COUNT(*) FILTER(WHERE provider='wildberries' AND id_type='sku' AND status='legacy'),COUNT(*) FILTER(WHERE provider='ozon' AND external_id='каламондин д9' AND status='active' AND is_primary AND source='manual') FROM product_external_ids`).Scan(&current, &legacy, &ozon))
			must(tx.QueryRow(ctx, "SELECT COUNT(*) FROM protected_business_data").Scan(&protected))
			wantCurrent, wantLegacy := 7, 4
			if scenario == "empty" {
				wantCurrent, wantLegacy = 4, 0
			}
			if current != wantCurrent || legacy != wantLegacy || ozon != 1 || protected != 5 {
				t.Fatalf("current=%d legacy=%d ozon=%d protected=%d", current, legacy, ozon, protected)
			}
			_, err = tx.Exec(ctx, "DROP TABLE wb_seven_repair_targets")
			must(err)
			_, err = tx.Exec(ctx, string(repair))
			must(err)
		})
	}
}
