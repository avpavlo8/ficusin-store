package procurement

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The procurement threshold is measured against the storefront variant, even
// when Saby has a different retail price. Each destination remains an action.
func TestProcurementPriceActionsUseSitePriceThresholdOnLiveDatabase(t *testing.T) {
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
	store := NewPostgresStore(pool)
	var threshold float64
	if err := pool.QueryRow(ctx, `SELECT price_change_threshold::DOUBLE PRECISION FROM procurement_pricing_settings WHERE id=1`).Scan(&threshold); err != nil {
		t.Fatal(err)
	}
	if threshold <= 0 || threshold >= 1 {
		t.Fatalf("test requires a positive configured threshold below 1, got %v", threshold)
	}
	unique := time.Now().UnixNano()
	var actorID, supplierID int64
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Threshold owner',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("threshold-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency)
		VALUES($1,'domestic','RUB') RETURNING id`, fmt.Sprintf("Threshold supplier %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_suppliers WHERE id=$1`, supplierID)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID)
	})
	// Use a large baseline so rounding to whole rubles cannot cross the
	// configured threshold by accident.
	const sitePrice = int64(100000)
	cases := []struct {
		name       string
		proposed   int64
		sabyPrice  int64
		wantAction bool
	}{
		{"inside_site_threshold_despite_different_saby_price", sitePrice + int64(math.Floor(float64(sitePrice)*threshold/2)), sitePrice * 2, false},
		{"above_site_threshold_even_when_saby_already_matches", sitePrice + int64(math.Ceil(float64(sitePrice)*(threshold+0.05))), 0, true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sabyID := fmt.Sprintf("site-threshold-%d-%d", unique, i)
			var productID, variantID, orderID int64
			t.Cleanup(func() {
				if orderID != 0 {
					_, _ = pool.Exec(ctx, `DELETE FROM procurement_orders WHERE id=$1`, orderID)
				}
				if productID != 0 {
					_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
				}
				_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID)
			})
			sabyPrice := tc.sabyPrice
			if sabyPrice == 0 {
				sabyPrice = tc.proposed
			}
			if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,price_minor)
				VALUES($1,$1,'Threshold plant',0,$2)`, sabyID, sabyPrice*100); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section)
				VALUES('Threshold plant',$1,'draft','plants') RETURNING id`, fmt.Sprintf("%d", unique+int64(i))).Scan(&productID); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,label,base_price_minor,is_active)
				VALUES($1,$2,'D12',$3,1) RETURNING id`, productID, sabyID, sitePrice*100).Scan(&variantID); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,created_by)
				VALUES($1,$2,'payment_invoice','RUB','ready_to_receive',$3) RETURNING id`, supplierID, fmt.Sprintf("SITE-THRESHOLD-%d-%d", unique, i), actorID).Scan(&orderID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,saby_id,canonical_variant_id,raw_name,
				ordered_qty,invoiced_qty,unit_price,match_status,reconciliation_status,proposed_retail_rub,baseline_site_price_minor)
				VALUES($1,$2,$3,'Threshold plant',1,1,100,'confirmed','matched',$4,$5)`, orderID, sabyID, variantID, tc.proposed, sitePrice*100); err != nil {
				t.Fatal(err)
			}
			batch, err := store.PrepareBatch(ctx, Actor{CustomerID: actorID, Role: "owner"}, orderID, "prices", []string{"site"})
			if err != nil {
				t.Fatal(err)
			}
			// Simulate site approval before preparing the next destination. The
			// invoice baseline must remain the same for the Saby suggestion.
			if tc.wantAction {
				if _, err := pool.Exec(ctx, `UPDATE product_variants SET base_price_minor=$2 WHERE id=$1`, variantID, tc.proposed*100); err != nil {
					t.Fatal(err)
				}
			}
			sabyBatch, err := store.PrepareBatch(ctx, Actor{CustomerID: actorID, Role: "owner"}, orderID, "prices", []string{"saby_price"})
			if err != nil {
				t.Fatal(err)
			}
			items := map[string]ActionItem{}
			for _, item := range append(batch.Items, sabyBatch.Items...) {
				items[item.Channel] = item
			}
			if !tc.wantAction {
				if len(items) != 0 {
					t.Fatalf("site price change within threshold created actions: %+v", batch.Items)
				}
				return
			}
			if len(items) != 2 {
				t.Fatalf("expected separate site and Saby actions, got %+v", batch.Items)
			}
			if site := items["site"]; site.OldValue == nil || *site.OldValue != float64(sitePrice) || site.NewValue != float64(tc.proposed) {
				t.Fatalf("site action should compare with storefront price: %+v", site)
			}
			if saby := items["saby_price"]; saby.OldValue == nil || *saby.OldValue != float64(sabyPrice) {
				t.Fatalf("Saby action should retain its own old price: %+v", saby)
			}
		})
	}
}
