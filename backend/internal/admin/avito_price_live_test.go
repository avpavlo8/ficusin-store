package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	if err = tx.QueryRow(ctx, `INSERT INTO products(name,slug,status,saby_fields) VALUES('Avito price test',$1,'published',ARRAY['stock']) RETURNING id`, fmt.Sprintf("avito-price-%d", unique)).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	sabyIDs := []string{fmt.Sprintf("avito-price-%d-1", unique), fmt.Sprintf("avito-price-%d-2", unique), fmt.Sprintf("avito-price-%d-3", unique)}
	for index, sabyID := range sabyIDs {
		if _, err = tx.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name) VALUES($1,$2,'Avito price test')`, sabyID, fmt.Sprintf("X%d", unique+int64(index))); err != nil {
			t.Fatal(err)
		}
	}
	for _, variant := range []struct {
		label  string
		price  int64
		id     *int64
		sabyID string
	}{
		{"D12", 100000, &cheapID, sabyIDs[0]}, {"D17", 150000, &expensiveID, sabyIDs[1]}, {"D20", 50000, &unavailableID, sabyIDs[2]},
	} {
		if err = tx.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor,is_active)
			VALUES($1,$2,$3,$4,$5,1) RETURNING id`, productID, variant.sabyID, fmt.Sprintf("8%017d", unique%100000000000000000+int64(variant.price)), variant.label, variant.price).Scan(variant.id); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.QueryRow(ctx, `SELECT id FROM warehouses WHERE saby_id='saby-ryazan-main' AND is_active=1 LIMIT 1`).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	for _, stock := range []struct {
		variantID int64
		qty       int
	}{{cheapID, 1}, {expensiveID, 1}, {unavailableID, 0}} {
		if _, err = tx.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,synced_at) VALUES($1,$2,$3,CURRENT_TIMESTAMP)`, warehouseID, stock.variantID, stock.qty); err != nil {
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
	// A stale cheaper size cannot hold down the proposed listing price.
	if _, err = tx.Exec(ctx, `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP-INTERVAL '3 hours' WHERE variant_id=$1`, cheapID); err != nil {
		t.Fatal(err)
	}
	if err = createManualPriceProposals(ctx, tx, Actor{CustomerID: actorID, Role: RoleOwner}, expensiveID, 150000, 160000); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT item.new_value::float8 FROM procurement_action_items item
		JOIN procurement_action_batches batch ON batch.id=item.batch_id
		WHERE batch.manual_variant_id=$1 AND item.channel='avito' AND item.status='draft'
		ORDER BY item.id DESC LIMIT 1`, expensiveID).Scan(&price); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale linked size should suppress Avito proposal, got price %.2f, err %v", price, err)
	}
	// With no trustworthy size, the proposal must still be absent; never 0.
	if _, err = tx.Exec(ctx, `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP-INTERVAL '3 hours' WHERE variant_id=$1`, expensiveID); err != nil {
		t.Fatal(err)
	}
	if err = createManualPriceProposals(ctx, tx, Actor{CustomerID: actorID, Role: RoleOwner}, expensiveID, 150000, 160000); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_action_items item
		JOIN procurement_action_batches batch ON batch.id=item.batch_id
		WHERE batch.manual_variant_id=$1 AND item.channel='avito' AND item.status='draft'`, expensiveID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("created %d Avito price proposals with stale stock", count)
	}
}
