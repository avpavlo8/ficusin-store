package avito

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAvitoUsesOnlyFreshSabyStockOnLiveDatabase(t *testing.T) {
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
	unique := time.Now().UnixNano()
	sabyID := fmt.Sprintf("avito-fresh-%d", unique)
	otherSabyID := sabyID + "-other"
	listingID := fmt.Sprintf("avito-fresh-listing-%d", unique)
	var productID, cheapID, availableID, warehouseID int64
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM avito_listings WHERE item_id=$1`, listingID)
		if productID != 0 {
			_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID)
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, otherSabyID)
	}()
	if err = pool.QueryRow(ctx, `SELECT id FROM warehouses WHERE saby_id='saby-ryazan-main' AND is_active=1 LIMIT 1`).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name) VALUES($1,$2,'Avito fresh test')`, sabyID, fmt.Sprintf("X%d", unique)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name) VALUES($1,$2,'Avito fresh test')`, otherSabyID, fmt.Sprintf("X%d", unique+1)); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,saby_fields) VALUES('Avito fresh test',$1,'published',ARRAY['stock']) RETURNING id`, fmt.Sprintf("avito-fresh-%d", unique)).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id     *int64
		price  int64
		suffix int
		sabyID string
	}{{&cheapID, 100000, 1, sabyID}, {&availableID, 150000, 2, otherSabyID}} {
		if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor,is_active) VALUES($1,$2,$3,'12 см',$4,1) RETURNING id`, productID, item.sabyID, fmt.Sprintf("9%017d", unique%10000000000000000+int64(item.suffix)), item.price).Scan(item.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,synced_at) VALUES($1,$2,5,CURRENT_TIMESTAMP-INTERVAL '3 hours'),($1,$3,2,CURRENT_TIMESTAMP)`, warehouseID, cheapID, availableID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO avito_listings(item_id,title,status) VALUES($1,'Avito fresh test','active')`, listingID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO avito_listing_products(item_id,product_id) VALUES($1,$2)`, listingID, productID); err != nil {
		t.Fatal(err)
	}
	service := New(pool, "", "", "https://ficusin.ru", "", "", "", "", "")
	find := func(items []Product) Product {
		for _, item := range items {
			if item.ID == productID {
				return item
			}
		}
		t.Fatalf("product %d not found", productID)
		return Product{}
	}
	products, err := service.SearchProducts(ctx, "Avito fresh test")
	if err != nil {
		t.Fatal(err)
	}
	if item := find(products); item.Price != 1500 || item.Stock != 2 {
		t.Fatalf("search product=%+v, want price 1500 and stock 2", item)
	}
	listings, _, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listingFound := false
	for _, listing := range listings {
		if listing.ItemID == listingID {
			listingFound = true
			if item := find(listing.Products); item.Price != 1500 || item.Stock != 2 {
				t.Fatalf("listing product=%+v, want price 1500 and stock 2", item)
			}
		}
	}
	if !listingFound {
		t.Fatalf("listing %s not found", listingID)
	}
	// Restore the integration state after this committed-fixture test.
	var priorStatus string
	var priorSuccess *time.Time
	if err = pool.QueryRow(ctx, `SELECT status,last_success_at FROM procurement_integration_sync_state
		WHERE channel='saby' AND resource='catalog'`).Scan(&priorStatus, &priorSuccess); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `UPDATE procurement_integration_sync_state SET status=$1,last_success_at=$2
			WHERE channel='saby' AND resource='catalog'`, priorStatus, priorSuccess)
	}()
	if _, err = pool.Exec(ctx, `UPDATE procurement_integration_sync_state SET status='ok',last_success_at=CURRENT_TIMESTAMP
		WHERE channel='saby' AND resource='catalog'`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP WHERE variant_id=$1`, cheapID); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	sender := New(pool, "client", "secret", "https://ficusin.ru", "", "", "", "", "")
	sender.baseURL = server.URL
	sender.client = server.Client()
	// 1500 was previously the minimum. A newly available 1000-ruble size
	// makes that approved price obsolete, even though all snapshots are fresh.
	err = sender.UpdatePrice(ctx, listingID, 1500)
	if err == nil || !strings.Contains(err.Error(), "пересчитайте предложение") {
		t.Fatalf("outdated price should require a new proposal, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("sent %d external requests for an outdated Avito price", calls)
	}
	err = sender.UpdatePrice(ctx, listingID, 1000.01)
	if err == nil || !strings.Contains(err.Error(), "пересчитайте предложение") || calls != 0 {
		t.Fatalf("one-kopeck mismatch reached Avito: err=%v calls=%d", err, calls)
	}
}
