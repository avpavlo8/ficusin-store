package admin

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The overview makes several joins across catalogue, procurement and Avito
// tables. Keep this as a database test; a handler stub cannot validate them.
func TestProductLinksAcrossCardAndSizesOnLiveDatabase(t *testing.T) {
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
	sabyID := fmt.Sprintf("links-saby-%d", unique)
	listingID := fmt.Sprintf("links-avito-%d", unique)
	var productID, firstID, secondID int64
	// ListProductLinks uses a pool, so fixtures must be committed and cleaned up
	// after the assertion rather than hidden in an uncommitted transaction.
	defer func() {
		if listingID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM avito_listings WHERE item_id=$1`, listingID)
		}
		if productID != 0 {
			_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID)
	}()
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name) VALUES($1,$2,'Links test')`, sabyID, fmt.Sprintf("X%d", unique)); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status) VALUES('Links test',$1,'draft') RETURNING id`, fmt.Sprintf("links-test-%d", unique)).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor,is_active)
		VALUES($1,$2,$3,'12 см',100000,1) RETURNING id`, productID, sabyID, fmt.Sprintf("7%017d", unique%100000000000000000)).Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active)
		VALUES($1,$2,'18 см',150000,1) RETURNING id`, productID, fmt.Sprintf("6%017d", unique%100000000000000000)).Scan(&secondID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO procurement_product_channels(saby_id,wb_nm_id,ozon_offer_id) VALUES($1,$2,$3)`, sabyID, unique%1000000000, fmt.Sprintf("ozon-%d", unique)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,status,is_primary,source)
		VALUES($1,$2,'wildberries','vendor_code',$3,'active',TRUE,'manual'),
		($1,$2,'ozon','offer_id',$4,'legacy',FALSE,'manual')`, productID, firstID, fmt.Sprintf("wb-%d", unique), fmt.Sprintf("old-ozon-%d", unique)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO avito_listings(item_id,title,status) VALUES($1,'Фикус, несколько размеров','active')`, listingID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO avito_listing_products(item_id,product_id) VALUES($1,$2)`, listingID, productID); err != nil {
		t.Fatal(err)
	}

	products, err := NewPostgresRepository(pool).ListProductLinks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found *ProductLinks
	for i := range products {
		if products[i].ID == productID {
			found = &products[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("product %d absent from overview", productID)
	}
	if len(found.Variants) != 2 {
		t.Fatalf("variants=%d, want 2", len(found.Variants))
	}
	if !found.Channels["avito"].Linked || len(found.Channels["avito"].ExternalIDs) != 1 || found.Channels["avito"].ExternalIDs[0] != listingID || found.Channels["avito"].ListingNames[0] != "Фикус, несколько размеров" {
		t.Fatalf("Avito card link=%+v", found.Channels["avito"])
	}
	byID := map[int64]VariantLinks{}
	for _, variant := range found.Variants {
		byID[variant.ID] = variant
	}
	first, second := byID[firstID], byID[secondID]
	for _, channel := range []string{"saby", "wb", "ozon"} {
		if !first.Channels[channel].Linked || !found.Channels[channel].Linked {
			t.Fatalf("first size/card missing %s link", channel)
		}
		if second.Channels[channel].Linked {
			t.Fatalf("second size unexpectedly linked to %s: %+v", channel, second.Channels[channel])
		}
	}
	for _, value := range first.Channels["ozon"].ExternalIDs {
		if value == fmt.Sprintf("old-ozon-%d", unique) {
			t.Fatal("legacy Ozon ID exposed as active link")
		}
	}
}
