package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage13SabyCatalogueMirrorIncludesUnimportedProductsOnLiveDatabase(t *testing.T) {
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

	var supplierID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM procurement_suppliers WHERE active ORDER BY id LIMIT 1`).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}

	unique := time.Now().UnixNano()
	folderID := fmt.Sprintf("ci-folder-%d", unique)
	sabyID := fmt.Sprintf("ci-saby-%d", unique)
	code := fmt.Sprintf("XCI%d", unique)
	name := "Антуриум Блэк Бьюти CI"

	if _, err := pool.Exec(ctx, `
		INSERT INTO saby_catalog_folders(saby_id,parent_saby_id,name,path,seen_at,missing_since)
		VALUES($1,NULL,'Цветы маркетплейс',ARRAY['Цветы маркетплейс'],CURRENT_TIMESTAMP,NULL)
	`, folderID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID)
		_, _ = pool.Exec(ctx, `DELETE FROM saby_catalog_folders WHERE saby_id=$1`, folderID)
	}()

	if _, err := pool.Exec(ctx, `
		INSERT INTO saby_nomenclature(
			saby_id,code,name,price_minor,balance,section_path,folder_saby_id,seen_at,missing_since
		) VALUES($1,$2,$3,395000,4,ARRAY['Цветы маркетплейс'],$4,CURRENT_TIMESTAMP,NULL)
	`, sabyID, code, name, folderID); err != nil {
		t.Fatal(err)
	}

	store := NewPostgresStore(pool)
	items, err := store.ListProducts(ctx, supplierID, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items=%d, want 1: %+v", len(items), items)
	}
	item := items[0]
	if item.SabyID != sabyID || item.SabyCode != code || item.Name != name {
		t.Fatalf("wrong mirror item: %+v", item)
	}
	if item.VariantID != 0 || item.SiteStatus != "" {
		t.Fatalf("unimported Saby item pretended to exist on site: %+v", item)
	}
	if item.FolderID != folderID || len(item.SectionPath) != 1 || item.SectionPath[0] != "Цветы маркетплейс" {
		t.Fatalf("folder identity lost: %+v", item)
	}
	if item.Balance != 4 || item.CurrentPriceRUB != 3950 {
		t.Fatalf("Saby state lost: %+v", item)
	}

	folders, err := store.ListSabyCatalogFolders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, folder := range folders {
		if folder.ID == folderID {
			found = folder.Name == "Цветы маркетплейс" && len(folder.Path) == 1
			break
		}
	}
	if !found {
		t.Fatalf("folder %s missing from mirror: %+v", folderID, folders)
	}
}


func TestInitialMarketplacePriceWorksBeforeSiteImportOnLiveDatabase(t *testing.T) {
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

	var actorID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM customers WHERE email='crm-owner@example.invalid'`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}

	unique := time.Now().UnixNano()
	sabyID := fmt.Sprintf("ci-marketplace-%d", unique)
	code := fmt.Sprintf("XMP%d", unique)
	wbNmID := int64(700000000 + unique%100000000)
	wbVendor := fmt.Sprintf("beauty-black-%d", unique)
	ozonOffer := fmt.Sprintf("beauty-black-ozon-%d", unique)
	var supplierID, orderID int64

	if err = pool.QueryRow(ctx, `
		INSERT INTO procurement_suppliers(name,kind,default_currency)
		VALUES($1,'international','EUR') RETURNING id
	`, fmt.Sprintf("Marketplace price supplier %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO saby_nomenclature(saby_id,code,name,balance,price_minor,section_path,seen_at)
		VALUES($1,$2,'Антуриум Блэк Бьюти CI',4,395000,ARRAY['Цветы маркетплейс'],CURRENT_TIMESTAMP)
	`, sabyID, code); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if orderID > 0 {
			_, _ = pool.Exec(ctx, `DELETE FROM procurement_orders WHERE id=$1`, orderID)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_product_channels WHERE saby_id=$1`, sabyID)
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_supplier_products WHERE saby_id=$1`, sabyID)
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_channel_products WHERE channel='wb' AND external_id=$1`, fmt.Sprint(wbNmID))
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID)
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_suppliers WHERE id=$1`, supplierID)
	}()

	if _, err = pool.Exec(ctx, `
		INSERT INTO procurement_channel_products(channel,external_id,article,name,seen_at)
		VALUES('wb',$1,$2,'Антуриум Блэк Бьюти CI',CURRENT_TIMESTAMP)
	`, fmt.Sprint(wbNmID), wbVendor); err != nil {
		t.Fatal(err)
	}

	store := NewPostgresStore(pool)
	if _, err = store.UpdateProduct(ctx, Actor{CustomerID: actorID}, ProductDirectoryUpdate{
		SabyID: sabyID, SupplierID: supplierID, WBVendorCode: wbVendor, OzonOfferID: ozonOffer,
		AvailabilityStatus: "available", MinimumOrderQty: 1, OrderMultiple: 1,
	}); err != nil {
		t.Fatal(err)
	}
	var keptWBNmID *int64
	if err = pool.QueryRow(ctx, `SELECT wb_nm_id FROM procurement_product_channels WHERE saby_id=$1`, sabyID).Scan(&keptWBNmID); err != nil || keptWBNmID == nil || *keptWBNmID != wbNmID {
		t.Fatalf("WB nmID was not resolved from seller article: nmID=%v err=%v", keptWBNmID, err)
	}

	if err = pool.QueryRow(ctx, `
		INSERT INTO procurement_orders(
			supplier_id,order_number,source_kind,currency,status,created_by,calculated_at,calculation_version
		) VALUES($1,$2,'invoice','EUR','ready_to_receive',$3,CURRENT_TIMESTAMP,4) RETURNING id
	`, supplierID, fmt.Sprintf("PRICE-%d", unique), actorID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO procurement_order_lines(
			procurement_order_id,saby_id,raw_name,ordered_qty,invoiced_qty,unit_price,
			unit_cost_rub,proposed_retail_rub,proposed_marketplace_rub,proposed_marketplace_strike_rub,
			match_status,reconciliation_status
		) VALUES($1,$2,'Антуриум Блэк Бьюти CI',4,4,12.5,1200,2390,3990,4990,'confirmed','matched')
	`, orderID, sabyID); err != nil {
		t.Fatal(err)
	}

	items, err := store.ListProducts(ctx, supplierID, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items=%d, want 1: %+v", len(items), items)
	}
	item := items[0]
	if item.VariantID != 0 {
		t.Fatalf("test product unexpectedly has site variant: %+v", item)
	}
	if item.WBNmID == nil || *item.WBNmID != wbNmID || item.WBVendorCode != wbVendor || item.OzonOfferID != ozonOffer {
		t.Fatalf("marketplace mapping lost for Saby-only product: %+v", item)
	}
	if item.SuggestedMarketplaceRUB == nil || *item.SuggestedMarketplaceRUB != 3990 ||
		item.SuggestedMarketplaceStrikeRUB == nil || *item.SuggestedMarketplaceStrikeRUB != 4990 {
		t.Fatalf("initial marketplace price missing: %+v", item)
	}

	batch, err := store.PrepareBatch(ctx, Actor{CustomerID: actorID}, orderID, "prices", []string{"wb", "ozon"})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Items) != 2 {
		t.Fatalf("price batch items=%d, want 2: %+v", len(batch.Items), batch.Items)
	}
	seen := map[string]ActionItem{}
	for _, action := range batch.Items {
		seen[action.Channel] = action
	}
	if seen["wb"].ExternalArticle != fmt.Sprint(wbNmID) || seen["wb"].NewValue != 3990 {
		t.Fatalf("WB initial price action wrong: %+v", seen["wb"])
	}
	if seen["ozon"].ExternalArticle != ozonOffer || seen["ozon"].NewValue != 3990 {
		t.Fatalf("Ozon initial price action wrong: %+v", seen["ozon"])
	}
}
