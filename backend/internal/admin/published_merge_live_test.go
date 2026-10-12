package admin

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This test runs against the migrated, isolated crm-live PostgreSQL service.
// It intentionally exercises the real FK/constraint triggers that unit tests
// cannot emulate. All fixture rows are removed even when an assertion fails.
func TestPublishedMergeOnLiveDatabase(t *testing.T) {
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
	targetSlug := fmt.Sprintf("merge-target-%d", unique)
	sourceSlug := fmt.Sprintf("merge-source-%d", unique)
	targetSaby := fmt.Sprintf("merge-target-saby-%d", unique)
	sourceSaby := fmt.Sprintf("merge-source-saby-%d", unique)
	targetSKU := fmt.Sprintf("%d1", unique)
	sourceSKU := fmt.Sprintf("%d2", unique)
	ownerEmail := fmt.Sprintf("merge-owner-%d@example.invalid", unique)
	orderNumber := fmt.Sprintf("MERGE-%d", unique)
	avitoID := fmt.Sprintf("merge-avito-%d", unique)
	var targetID, sourceID, targetCode, sourceCode, targetVariantID, sourceVariantID, ownerID, orderID, reviewID, orderItemID int64
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE action='product.published.merge' AND entity_id=$1`, fmt.Sprint(targetID))
		_, _ = pool.Exec(ctx, `DELETE FROM product_reviews WHERE id=$1`, reviewID)
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM avito_listings WHERE item_id=$1`, avitoID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id IN ($1,$2)`, targetID, sourceID)
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id IN ($1,$2)`, targetSaby, sourceSaby)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, ownerID)
	}()
	var categoryID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM categories WHERE slug='aglaonema'`).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Merge test owner',CURRENT_TIMESTAMP) RETURNING id`, ownerEmail, fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name) VALUES
		($1,$2,'Аглаонема тест D12'),($3,$4,'Аглаонема тест D17')`,
		targetSaby, fmt.Sprintf("X%d1", unique), sourceSaby, fmt.Sprintf("X%d2", unique)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO products(saby_id,slug,name,latin_name,status,catalog_section,category_id,care_level)
		VALUES($1,$2,'Тестовая аглаонема Raven D12','Aglaonema commutatum Raven','published','plants',$3,'easy')
		RETURNING id,product_code`, targetSaby, targetSlug, categoryID).Scan(&targetID, &targetCode); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO products(saby_id,slug,name,latin_name,status,catalog_section,category_id,plant_kind,light_level,care_level,short_description)
		VALUES($1,$2,'Тестовая аглаонема Raven D17','Aglaonema commutatum Raven','published','plants',$3,'aglaonema','diffused','demanding','Проверенное описание')
		RETURNING id,product_code`, sourceSaby, sourceSlug, categoryID).Scan(&sourceID, &sourceCode); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor,is_active)
		VALUES($1,$2,$3,'D12',120000,1) RETURNING id`, targetID, targetSaby, targetSKU).Scan(&targetVariantID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor,is_active)
		VALUES($1,$2,$3,'D17',170000,1) RETURNING id`, sourceID, sourceSaby, sourceSKU).Scan(&sourceVariantID); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		productID  int64
		externalID string
	}{
		{targetID, targetSlug + "-offer"}, {sourceID, sourceSlug + "-offer"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO product_external_ids(product_id,provider,id_type,external_id,status,is_primary)
			VALUES($1,'ozon','offer_id',$2,'active',TRUE)`, item.productID, item.externalID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO product_media(product_id,object_key,alt_text) VALUES($1,$2,'Merge test')`, sourceID, "https://example.invalid/"+sourceSlug+".jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO collection_products(collection_id,product_id)
		SELECT id,$1 FROM collections WHERE slug='easy'`, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO avito_listings(item_id,title) VALUES($1,'Merge test')`, avitoID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO avito_listing_products(item_id,product_id) VALUES($1,$2)`, avitoID, sourceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status)
		VALUES($1,$2,'Merge test','+70000000000',$3,'pickup',0,1700,1700,'new') RETURNING id`, orderNumber, ownerID, ownerEmail).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,unit_price,quantity)
		VALUES($1,$2,$3,$4,'Тестовая аглаонема Raven D17','D17',1700,1) RETURNING id`, orderID, sourceID, sourceVariantID, sourceSKU).Scan(&orderItemID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO product_reviews(product_id,customer_id,order_id,rating,body,status)
		VALUES($1,$2,$3,5,'Хорошее тестовое растение','published') RETURNING id`, sourceID, ownerID, orderID).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(pool)
	request := PublishedMergeRequest{TargetProductID: targetID, SourceProductIDs: []int64{sourceID},
		ExpectedSKUs: []string{targetSKU, sourceSKU}, ReviewedIdentity: "Тестовая аглаонема Raven"}
	actor := Actor{Role: RoleOwner, CustomerID: ownerID}
	blockedPreview, err := repository.PreviewPublishedMerge(ctx, actor, request)
	if err != nil || blockedPreview.Ready || len(blockedPreview.Blockers) == 0 {
		t.Fatalf("conflicting PIM must block read-only preview: %+v, %v", blockedPreview, err)
	}
	if !strings.Contains(blockedPreview.Blockers[0], "care_level") ||
		!strings.Contains(blockedPreview.Blockers[0], fmt.Sprintf(`"productId": %d`, sourceID)) {
		t.Fatalf("preview must identify the conflicting attribute and product: %+v", blockedPreview.Blockers)
	}
	if err := repository.MergePublishedProducts(ctx, actor, request); err == nil {
		t.Fatal("conflicting PIM care_level was accepted")
	}
	var stillSource int64
	if err := pool.QueryRow(ctx, `SELECT product_id FROM product_variants WHERE id=$1`, sourceVariantID).Scan(&stillSource); err != nil || stillSource != sourceID {
		t.Fatalf("failed merge changed variant owner: %d, %v", stillSource, err)
	}
	if err := pool.QueryRow(ctx, `SELECT product_id FROM product_external_ids WHERE external_id=$1`, sourceSlug+"-offer").Scan(&stillSource); err != nil || stillSource != sourceID {
		t.Fatalf("failed merge changed external ID: %d, %v", stillSource, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE products SET care_level='easy' WHERE id=$1`, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO launch_hidden_extra_plant_products(product_id,excluded_sku,previous_status)
		VALUES($1,$2,'published')`, sourceID, sourceSKU); err != nil {
		t.Fatal(err)
	}
	if preview, err := repository.PreviewPublishedMerge(ctx, actor, request); err != nil || preview.Ready || len(preview.Blockers) == 0 {
		t.Fatalf("hidden-card audit must block merge: %+v, %v", preview, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM launch_hidden_extra_plant_products WHERE product_id=$1`, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO launch_hidden_extra_plant_variants(variant_id,product_id,excluded_sku)
		VALUES($1,$2,$3)`, sourceVariantID, sourceID, sourceSKU); err != nil {
		t.Fatal(err)
	}
	if preview, err := repository.PreviewPublishedMerge(ctx, actor, request); err != nil || preview.Ready || len(preview.Blockers) == 0 {
		t.Fatalf("hidden-variant audit must block merge: %+v, %v", preview, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM launch_hidden_extra_plant_variants WHERE variant_id=$1`, sourceVariantID); err != nil {
		t.Fatal(err)
	}
	readyPreview, err := repository.PreviewPublishedMerge(ctx, actor, request)
	if err != nil || !readyPreview.Ready || len(readyPreview.Blockers) != 0 {
		t.Fatalf("valid group should pass read-only preview: %+v, %v", readyPreview, err)
	}
	if err := pool.QueryRow(ctx, `SELECT product_id FROM product_variants WHERE id=$1`, sourceVariantID).Scan(&stillSource); err != nil || stillSource != sourceID {
		t.Fatalf("preview changed variant owner: %d, %v", stillSource, err)
	}
	if err := repository.MergePublishedProducts(ctx, actor, request); err != nil {
		t.Fatalf("merge with live constraints: %v", err)
	}
	var variantCount, sourceCount, aliasCount, collectionCount, avitoCount, photoCount, externalCount, auditCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM product_variants WHERE product_id=$1`, targetID).Scan(&variantCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM products WHERE id=$1`, sourceID).Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM product_url_aliases WHERE product_id=$1 AND alias IN ($2,$3)`, targetID, fmt.Sprint(sourceCode), sourceSlug).Scan(&aliasCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM collection_products WHERE product_id=$1`, targetID).Scan(&collectionCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM avito_listing_products WHERE item_id=$1 AND product_id=$2`, avitoID, targetID).Scan(&avitoCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM product_media WHERE product_id=$1 AND variant_id=$2`, targetID, sourceVariantID).Scan(&photoCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM product_external_ids WHERE product_id=$1 AND variant_id=$2 AND external_id=$3 AND status='active'`, targetID, sourceVariantID, sourceSlug+"-offer").Scan(&externalCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM admin_audit_log WHERE action='product.published.merge' AND entity_id=$1`, fmt.Sprint(targetID)).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if variantCount != 2 || sourceCount != 0 || aliasCount != 2 || collectionCount != 1 || avitoCount != 1 || photoCount != 1 || externalCount != 1 || auditCount != 1 {
		t.Fatalf("lost merged data: variants=%d source=%d aliases=%d collection=%d avito=%d photo=%d external=%d audit=%d", variantCount, sourceCount, aliasCount, collectionCount, avitoCount, photoCount, externalCount, auditCount)
	}
	var name, plantKind, light, description string
	if err := pool.QueryRow(ctx, `SELECT name,plant_kind,light_level,short_description FROM products WHERE id=$1`, targetID).Scan(&name, &plantKind, &light, &description); err != nil {
		t.Fatal(err)
	}
	if name != "Тестовая аглаонема Raven" || plantKind != "aglaonema" || light != "diffused" || description != "Проверенное описание" {
		t.Fatalf("lost editorial content: name=%q kind=%q light=%q description=%q", name, plantKind, light, description)
	}
	var orderProductID, reviewProductID, orderVariantID int64
	var orderSKU string
	if err := pool.QueryRow(ctx, `SELECT product_id,variant_id,sku FROM order_items WHERE id=$1`, orderItemID).Scan(&orderProductID, &orderVariantID, &orderSKU); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT product_id FROM product_reviews WHERE id=$1`, reviewID).Scan(&reviewProductID); err != nil {
		t.Fatal(err)
	}
	if orderProductID != targetID || orderVariantID != sourceVariantID || orderSKU != sourceSKU || reviewProductID != targetID {
		t.Fatalf("history changed: order=%d/%d/%s review=%d", orderProductID, orderVariantID, orderSKU, reviewProductID)
	}
	var preservedCode int64
	if err := pool.QueryRow(ctx, `SELECT product_code FROM products WHERE id=$1`, targetID).Scan(&preservedCode); err != nil {
		t.Fatal(err)
	}
	if preservedCode != targetCode {
		t.Fatalf("target public code changed: %d -> %d", targetCode, preservedCode)
	}
}
