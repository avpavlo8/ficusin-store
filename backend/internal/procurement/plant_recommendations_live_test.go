package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage05SiteStockAndProcurementArrival(t *testing.T) {
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
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	store := NewPostgresStore(pool)
	key := fmt.Sprintf("procurement-plant-%d", time.Now().UnixNano())
	var productID, variantID, supplierID, orderID, lineID int64
	var productCode string
	_, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,section_path)
		VALUES($1,$1,'Внешнее название',9,ARRAY['Прочее'])`, key)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section,category_id,saby_fields)
		SELECT 'Тестовое растение',$1,'published','plants',id,ARRAY['stock'] FROM categories WHERE slug='indoor-plants'
		RETURNING id,product_code::TEXT`, key).Scan(&productID, &productCode))
	must(pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor,saby_id)
		VALUES($1,'D12',10000,$2) RETURNING id`, productID, key).Scan(&variantID))
	_, err = pool.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at)
		SELECT id,$1,0,0,CURRENT_TIMESTAMP FROM warehouses WHERE saby_id='saby-ryazan-main'`, variantID)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO procurement_plant_stock_days(variant_id,observed_date,in_stock)
		VALUES($1,(CURRENT_TIMESTAMP AT TIME ZONE 'Europe/Moscow')::DATE-1,TRUE),
			($1,(CURRENT_TIMESTAMP AT TIME ZONE 'Europe/Moscow')::DATE,FALSE)`, variantID)
	must(err)
	findRecommendation := func() Recommendation {
		t.Helper()
		items, err := store.listRecommendations(ctx, PricingSettings{RecommendationDays: 30, TargetCoverDays: 30})
		must(err)
		for _, item := range items {
			if item.SabyID == key {
				return item
			}
		}
		t.Fatal("plant missing from recommendations")
		return Recommendation{}
	}
	item := findRecommendation()
	if item.Status != RecommendationReady || item.QuantityKnown || !item.BalanceKnown ||
		item.Balance != 0 || item.SupplierID != 0 || item.InStockDays != 1 || item.ObservedStockDays != 2 {
		t.Fatalf("sold-out site plant = %+v", item)
	}
	var actorID int64
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at)
		VALUES($1,$2,'Procurement test',CURRENT_TIMESTAMP) RETURNING id`,
		key+"@example.invalid", fmt.Sprintf("+7999%07d", time.Now().UnixNano()%10000000)).Scan(&actorID))
	actor := Actor{CustomerID: actorID, Role: "owner"}
	must(store.SetExclusion(ctx, actor, ExclusionUpdate{SabyID: key, Excluded: true, Reason: "Разовая закупка"}))
	if item := findRecommendation(); item.Status != RecommendationExcluded || !item.HiddenFromRestock {
		t.Fatalf("hidden plant = %+v", item)
	}
	must(store.SetExclusion(ctx, actor, ExclusionUpdate{SabyID: key}))
	if item := findRecommendation(); item.Status != RecommendationReady || item.HiddenFromRestock {
		t.Fatalf("restored plant = %+v", item)
	}

	must(pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency)
		VALUES($1,'domestic','RUB') RETURNING id`, key).Scan(&supplierID))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,source_kind,currency,status,expected_arrival_date)
		VALUES($1,'payment_invoice','RUB','invoice_received',(CURRENT_TIMESTAMP AT TIME ZONE 'Europe/Moscow')::DATE+8)
		RETURNING id`, supplierID).Scan(&orderID))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,saby_id,canonical_variant_id,
		raw_name,invoiced_qty,match_status,reconciliation_status)
		VALUES($1,$2,$3,'Тестовое растение',4,'confirmed','matched') RETURNING id`,
		orderID, key, variantID).Scan(&lineID))
	repository := catalog.NewPostgresRepository(pool)
	checkArrival := func(expected bool) {
		t.Helper()
		detail, err := repository.DetailBySlug(ctx, productCode)
		must(err)
		products, err := repository.ListAvailable(ctx)
		must(err)
		for _, product := range products {
			if product.ID == productCode {
				if (detail.Variants[0].ExpectedArrivalDate != "") != expected ||
					(detail.Variants[0].ExpectedArrivalDate != product.ExpectedArrivalDate) {
					t.Fatalf("unexpected ETA: variant=%q, listing=%q", detail.Variants[0].ExpectedArrivalDate, product.ExpectedArrivalDate)
				}
				return
			}
		}
		t.Fatal("plant missing from the site catalogue")
	}
	checkArrival(true)
	_, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET invoice_excluded=TRUE WHERE id=$1`, lineID)
	must(err)
	checkArrival(false)
	_, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET invoice_excluded=FALSE WHERE id=$1`, lineID)
	must(err)
	_, err = pool.Exec(ctx, `UPDATE inventory SET available_qty=1 WHERE variant_id=$1`, variantID)
	must(err)
	checkArrival(false)
	_, err = pool.Exec(ctx, `UPDATE inventory SET available_qty=0 WHERE variant_id=$1`, variantID)
	must(err)
	_, err = pool.Exec(ctx, `UPDATE procurement_orders SET status='cancelled' WHERE id=$1`, orderID)
	must(err)
	checkArrival(false)
}
