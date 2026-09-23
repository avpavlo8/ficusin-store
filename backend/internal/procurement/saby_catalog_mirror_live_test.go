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

	unique := time.Now().UnixNano()
	sabyID := fmt.Sprintf("ci-marketplace-%d", unique)
	code := fmt.Sprintf("XMP%d", unique)
	wbNmID := int64(700000000 + unique%100000000)
	wbVendor := fmt.Sprintf("beauty-black-%d", unique)
	ozonOffer := fmt.Sprintf("beauty-black-ozon-%d", unique)

	var actorID, supplierID, orderID int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Marketplace price owner',CURRENT_TIMESTAMP) RETURNING id`,
		fmt.Sprintf("marketplace-price-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency)
		VALUES($1,'international','EUR') RETURNING id`, fmt.Sprintf("Marketplace price supplier %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,price_minor,section_path,seen_at)
		VALUES($1,$2,'Антуриум Блэк Бьюти CI',4,395000,ARRAY['Цветы маркетплейс'],CURRENT_TIMESTAMP)`, sabyID, code); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_orders WHERE id=$1`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_product_channels WHERE saby_id=$1`, sabyID)
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_supplier_products WHERE saby_id=$1`, sabyID)
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID)
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_suppliers WHERE id=$1`, supplierID)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID)
	}()

	if _, err = pool.Exec(ctx, `INSERT INTO procurement_channel_products(channel,external_id,article,name,seen_at)
		VALUES('wb',$1,$2,'Антуриум Блэк Бьюти CI',CURRENT_TIMESTAMP)`, fmt.Sprint(wbNmID), wbVendor); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_channel_products WHERE channel='wb' AND external_id=$1`, fmt.Sprint(wbNmID))
	}()
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

	if err = pool.QueryRow(ctx, `INSERT INTO procurement_orders(
		supplier_id,order_number,source_kind,currency,status,created_by,calculated_at,calculation_version
	) VALUES($1,$2,'invoice','EUR','ready_to_receive',$3,CURRENT_TIMESTAMP,4) RETURNING id`,
		supplierID, fmt.Sprintf("PRICE-%d", unique), actorID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO procurement_order_lines(
		procurement_order_id,saby_id,raw_name,ordered_qty,invoiced_qty,unit_price,
		unit_cost_rub,proposed_retail_rub,proposed_marketplace_rub,proposed_marketplace_strike_rub,
		match_status,reconciliation_status
	) VALUES($1,$2,'Антуриум Блэк Бьюти CI',4,4,12.5,1200,2390,3990,4990,'confirmed','matched')`,
		orderID, sabyID); err != nil {
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

	// The calculated marketplace price is product-level information. It must
	// stay visible even when the directory is currently filtered by another
	// supplier.
	var otherSupplierID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency)
		VALUES($1,'domestic','RUB') RETURNING id`, fmt.Sprintf("Marketplace price other supplier %d", unique)).Scan(&otherSupplierID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM procurement_suppliers WHERE id=$1`, otherSupplierID) }()
	otherItems, err := store.ListProducts(ctx, otherSupplierID, code)
	if err != nil || len(otherItems) != 1 || otherItems[0].SuggestedMarketplaceRUB == nil || *otherItems[0].SuggestedMarketplaceRUB != 3990 {
		t.Fatalf("latest marketplace price disappeared behind supplier filter: items=%+v err=%v", otherItems, err)
	}

	// A missing WB nmID must not silently remove the product from the draft.
	// The manager needs to see the calculated price and the exact missing link.
	if _, err = pool.Exec(ctx, `UPDATE procurement_action_batches SET status='cancelled' WHERE id=$1`, batch.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE procurement_product_channels SET wb_nm_id=NULL WHERE saby_id=$1`, sabyID); err != nil {
		t.Fatal(err)
	}
	missingBatch, err := store.PrepareBatch(ctx, Actor{CustomerID: actorID}, orderID, "prices", []string{"wb"})
	if err != nil {
		t.Fatal(err)
	}
	if len(missingBatch.Items) != 1 || missingBatch.Items[0].Channel != "wb" ||
		missingBatch.Items[0].ExternalArticle != "" || missingBatch.Items[0].NewValue != 3990 ||
		missingBatch.Items[0].ErrorMessage == "" {
		t.Fatalf("unlinked WB price was hidden instead of explained: %+v", missingBatch.Items)
	}
	if _, err = pool.Exec(ctx, `UPDATE procurement_action_batches SET status='cancelled' WHERE id=$1`, missingBatch.ID); err != nil {
		t.Fatal(err)
	}

	// When the fresh WB mirror later contains the card, the saved seller
	// article should backfill nmID automatically without asking the manager to
	// reopen and resave the product.
	if err = store.RememberChannelProducts(ctx, "wb", []ChannelProduct{{
		ExternalID: fmt.Sprint(wbNmID), Article: wbVendor, Name: "Антуриум Блэк Бьюти CI",
	}}); err != nil {
		t.Fatal(err)
	}
	keptWBNmID = nil
	if err = pool.QueryRow(ctx, `SELECT wb_nm_id FROM procurement_product_channels WHERE saby_id=$1`, sabyID).Scan(&keptWBNmID); err != nil || keptWBNmID == nil || *keptWBNmID != wbNmID {
		t.Fatalf("fresh WB mirror did not backfill nmID: nmID=%v err=%v", keptWBNmID, err)
	}
}
