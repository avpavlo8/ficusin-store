package procurement

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"math"
	"os"
	"testing"
	"time"
)

func TestUnifiedPreviewAndCalculationUseSameWeightedCostWithoutPublishing(t *testing.T) {
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
	live, err := NewPostgresStore(pool).loadSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !validateUnified(live.Economics.Unified, live.Economics.Channels, live.TaxRate) {
		t.Fatal("migrated pricing settings cannot calculate")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	tag := fmt.Sprint(time.Now().UnixNano())
	key := "unified-" + tag
	var product, variant, supplier, order int64
	must(tx.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section) VALUES('Unified pricing test',$1,'draft','plants') RETURNING id`, key).Scan(&product))
	must(tx.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,saby_id,is_active,base_price_minor) VALUES($1,'D12',$2,1,99900) RETURNING id`, product, key).Scan(&variant))
	_, err = tx.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,name) VALUES($1,'Unified pricing test')`, key)
	must(err)
	_, err = tx.Exec(ctx, `INSERT INTO procurement_pricing_packages(variant_id,dimensions) VALUES($1,'{"lengthCm":15,"widthCm":15,"heightCm":40,"source":"Test evidence"}')`, variant)
	must(err)
	must(tx.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'domestic','RUB') RETURNING id`, key).Scan(&supplier))
	must(tx.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status) VALUES($1,$2,'manual','RUB','received') RETURNING id`, supplier, key).Scan(&order))
	for _, v := range []struct {
		qty  int
		cost float64
	}{{2, 100}, {1, 400}} {
		_, err = tx.Exec(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,canonical_variant_id,saby_id,raw_name,ordered_qty,invoiced_qty,unit_cost_rub,match_status,reconciliation_status) VALUES($1,$2,$3,'Unified pricing test',$4,$4,$5,'confirmed','matched')`, order, variant, key, v.qty, v.cost)
		must(err)
	}
	s := unifiedSettings(t)
	s.Version = 123
	var before int
	must(tx.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_action_items`).Scan(&before))
	preview, err := loadUnifiedOrderPreview(ctx, tx, s, order)
	must(err)
	if len(preview.Lines) != 1 || preview.Units != 3 || preview.PricedUnits != 3 || preview.Lines[0].CostRUB != 200 || preview.ContributionRUB != 360 {
		t.Fatalf("wrong weighted preview: %+v", preview)
	}
	must(saveUnifiedOrderQuotes(ctx, tx, s, order))
	rows, err := tx.Query(ctx, `SELECT channel_quotes,unit_cost_rub::float8 FROM procurement_order_lines WHERE procurement_order_id=$1 ORDER BY id`, order)
	must(err)
	for rows.Next() {
		var quotes map[string]ChannelQuote
		var cost float64
		must(rows.Scan(&quotes, &cost))
		if *quotes["ozon"].PriceRUB != *preview.Lines[0].Quotes["ozon"].PriceRUB || math.Abs(*quotes["ozon"].ContributionRUB-120) > 1 {
			t.Fatal("preview differs from saved proposals")
		}
		if cost != 100 && cost != 400 {
			t.Fatal("acquisition costs changed")
		}
	}
	rows.Close()
	must(rows.Err())
	var after int
	var price int64
	must(tx.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_action_items`).Scan(&after))
	must(tx.QueryRow(ctx, `SELECT base_price_minor FROM product_variants WHERE id=$1`, variant).Scan(&price))
	if before != after || price != 99900 {
		t.Fatal("preview/calculation published a price or enqueued work")
	}
	// A complete PIM box wins; importing MP evidence must not change PIM.
	for _, code := range []string{"package_length_cm", "package_width_cm", "package_height_cm"} {
		_, err = tx.Exec(ctx, `INSERT INTO variant_attribute_values(variant_id,attribute_id,value,source) SELECT $1,id,'10'::jsonb,'import' FROM attribute_definitions WHERE code=$2 ON CONFLICT(variant_id,attribute_id) DO UPDATE SET value=EXCLUDED.value`, variant, code)
		must(err)
	}
	boxes, err := loadPricingPackages(ctx, tx, []int64{variant})
	must(err)
	if boxes[variant].Liters() != 1 || boxes[variant].Source != "PIM сайта" {
		raw, _ := json.Marshal(boxes)
		t.Fatalf("ignored PIM: %s", raw)
	}
	_, err = tx.Exec(ctx, `UPDATE procurement_order_lines SET unit_cost_rub=NULL WHERE procurement_order_id=$1 AND unit_cost_rub=400`, order)
	must(err)
	missing, err := loadUnifiedOrderPreview(ctx, tx, s, order)
	must(err)
	if missing.Units != 3 || missing.PricedUnits != 0 || len(missing.Lines) != 1 || missing.Lines[0].Quotes["wb"].PriceRUB != nil {
		t.Fatal("missing cost was silently dropped or treated as free")
	}
}
