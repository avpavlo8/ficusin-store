package procurement

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestProcurementChannelEditTargetsExactVariantOnLiveDatabase(t *testing.T) {
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
	tag := fmt.Sprint(time.Now().UnixNano())
	var product, small, large int64
	must(tx.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section,saby_id) VALUES('Variant channel test',$1,'draft','plants',$2) RETURNING id`, "channel-"+tag, "small-"+tag).Scan(&product))
	must(tx.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,saby_id,is_active) VALUES($1,'D9',$2,1) RETURNING id`, product, "small-"+tag).Scan(&small))
	must(tx.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,saby_id,is_active) VALUES($1,'D14',$2,1) RETURNING id`, product, "large-"+tag).Scan(&large))
	for _, row := range []struct {
		saby, offer string
		nm          int64
	}{{"small-" + tag, "small-offer-" + tag, 123}, {"large-" + tag, "large-offer-" + tag, 456}} {
		_, err = tx.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,name) VALUES($1,'Channel variant test')`, row.saby)
		must(err)
		_, err = tx.Exec(ctx, `INSERT INTO procurement_product_channels(saby_id,ozon_offer_id,wb_nm_id,wb_vendor_code) VALUES($1,$2,$3,$2) ON CONFLICT(saby_id) DO UPDATE SET ozon_offer_id=EXCLUDED.ozon_offer_id,wb_nm_id=EXCLUDED.wb_nm_id,wb_vendor_code=EXCLUDED.wb_vendor_code`, row.saby, row.offer, row.nm)
		must(err)
	}
	for _, row := range []struct {
		variant   int64
		offer, nm string
	}{{small, "small-offer-" + tag, "123"}, {large, "large-offer-" + tag, "456"}} {
		var offer, nm string
		must(tx.QueryRow(ctx, `SELECT ozon_articles[1],wb_nm_ids[1] FROM canonical_product_directory WHERE variant_id=$1`, row.variant).Scan(&offer, &nm))
		if offer != row.offer || nm != row.nm {
			t.Fatalf("variant %d got %q %q, want %q %q", row.variant, offer, nm, row.offer, row.nm)
		}
	}
}
