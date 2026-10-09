package admin

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// An Avito ad represents the card, but a manual price edit changes one size.
// The advertised price must remain the cheapest size currently in stock.
func TestManualAvitoPriceUsesCheapestAvailableVariantOnLiveDatabase(t *testing.T) {
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
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	unique := time.Now().UnixNano()
	var actorID, productID, cheapID, expensiveID, unavailableID, warehouseID int64
	if err = tx.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Avito price test',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("avito-price-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `INSERT INTO products(name,slug,status) VALUES('Avito price test',$1,'published') RETURNING id`, fmt.Sprintf("avito-price-%d", unique)).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []struct {
		label string
		price int64
		id    *int64
	}{
		{"D12", 100000, &cheapID}, {"D17", 150000, &expensiveID}, {"D20", 50000, &unavailableID},
	} {
		if err = tx.QueryRow(ctx, `INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active)
			VALUES($1,$2,$3,$4,1) RETURNING id`, productID, fmt.Sprintf("8%017d", unique%100000000000000000+int64(variant.price)), variant.label, variant.price).Scan(variant.id); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.QueryRow(ctx, `INSERT INTO warehouses(saby_id,name,city,address) VALUES($1,'Avito test','Рязань','Тест') RETURNING id`, fmt.Sprintf("avito-price-%d", unique)).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	for _, stock := range []struct {
		variantID int64
		qty       int
	}{{cheapID, 1}, {expensiveID, 1}, {unavailableID, 0}} {
		if _, err = tx.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty) VALUES($1,$2,$3)`, warehouseID, stock.variantID, stock.qty); err != nil {
			t.Fatal(err)
		}
	}
	listingID := fmt.Sprintf("price-test-%d", unique)
	if _, err = tx.Exec(ctx, `INSERT INTO avito_listings(item_id,title,status,remote_price_minor) VALUES($1,'Avito test','active',100000)`, listingID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO avito_listing_products(item_id,product_id) VALUES($1,$2)`, listingID, productID); err != nil {
		t.Fatal(err)
	}
	// Raising D17 must not raise the listing above the cheaper D12. D20 has no stock.
	if err = createManualPriceProposals(ctx, tx, Actor{CustomerID: actorID, Role: RoleOwner}, expensiveID, 150000, 160000); err != nil {
		t.Fatal(err)
	}
	var price float64
	if err = tx.QueryRow(ctx, `SELECT item.new_value::float8 FROM procurement_action_items item
		JOIN procurement_action_batches batch ON batch.id=item.batch_id
		WHERE batch.manual_variant_id=$1 AND item.channel='avito' AND item.external_article=$2`, expensiveID, listingID).Scan(&price); err != nil {
		t.Fatal(err)
	}
	if price != 1000 {
		t.Fatalf("Avito price = %.2f; want cheapest available size price 1000", price)
	}
}
