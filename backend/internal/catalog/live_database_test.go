package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRecommendationsQueryOnLiveDatabase(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	unique := time.Now().UnixNano()
	var categoryID int64
	if err := pool.QueryRow(ctx, `INSERT INTO categories(name,slug) VALUES($1,$2) RETURNING id`,
		"Интеграционные растения", fmt.Sprintf("catalog-plan-%d", unique)).Scan(&categoryID); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	for index := 0; index < 12; index++ {
		var seededProductID int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO products(category_id,name,slug,status,catalog_section,plant_kind,light_level,watering,height_class,care_level,placement,pet_safety,growth_habit)
			VALUES($1,$2,$3,'published','plants','foliage','bright','medium','medium','easy','home','safe','upright') RETURNING id`,
			categoryID, fmt.Sprintf("Плановый товар %d", index), fmt.Sprintf("catalog-plan-%d-%d", unique, index),
		).Scan(&seededProductID); err != nil {
			t.Fatalf("seed product %d: %v", index, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active) VALUES($1,$2,'Основной',100000,1)`,
			seededProductID, fmt.Sprintf("8%09d%02d", unique%1000000000, index)); err != nil {
			t.Fatalf("seed variant %d: %v", index, err)
		}
	}

	var productID int64
	var detail ProductDetail
	err = pool.QueryRow(ctx, `
		SELECT id,category_id,catalog_section,COALESCE(plant_kind,''),COALESCE(light_level,''),
			COALESCE(watering,''),COALESCE(height_class,''),COALESCE(care_level,''),
			COALESCE(placement,''),COALESCE(pet_safety,''),COALESCE(growth_habit,'')
		FROM products WHERE status='published' AND EXISTS(
			SELECT 1 FROM product_variants WHERE product_id=products.id AND is_active=1 AND archived_at IS NULL
		) ORDER BY id LIMIT 1
	`).Scan(&productID, &detail.CategoryID, &detail.CatalogSection, &detail.PlantKind, &detail.LightLevel,
		&detail.Watering, &detail.HeightClass, &detail.CareLevel, &detail.Placement, &detail.PetSafety, &detail.GrowthHabit)
	if err != nil {
		t.Fatalf("select recommendation source: %v", err)
	}
	arguments := []any{productID, detail.CategoryID, detail.CatalogSection, detail.PlantKind, detail.LightLevel,
		detail.Watering, detail.HeightClass, detail.CareLevel, detail.Placement, detail.PetSafety, detail.GrowthHabit}
	var rawPlan []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+recommendationsQuery, arguments...).Scan(&rawPlan); err != nil {
		t.Fatalf("explain recommendations: %v", err)
	}
	var plan any
	if err := json.Unmarshal(rawPlan, &plan); err != nil {
		t.Fatalf("decode explain plan: %v", err)
	}
	planText := string(rawPlan)
	for _, required := range []string{"Execution Time", "Shared Hit Blocks"} {
		if !strings.Contains(planText, required) {
			t.Fatalf("EXPLAIN output does not contain %q", required)
		}
	}
	t.Logf("recommendations EXPLAIN: %s", rawPlan)

	items, err := NewPostgresRepository(pool).listRecommendations(ctx, productID, detail)
	if err != nil {
		t.Fatalf("run recommendations: %v", err)
	}
	if len(items) > 8 {
		t.Fatalf("recommendations returned %d rows", len(items))
	}
}

func TestCatalogQueryPlanOnLiveDatabase(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var rawPlan []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+catalogListQuery).Scan(&rawPlan); err != nil {
		t.Fatalf("explain catalog: %v", err)
	}
	var plan any
	if err := json.Unmarshal(rawPlan, &plan); err != nil {
		t.Fatalf("decode catalog plan: %v", err)
	}
	for _, required := range []string{"Execution Time", "Shared Hit Blocks"} {
		if !strings.Contains(string(rawPlan), required) {
			t.Fatalf("EXPLAIN output does not contain %q", required)
		}
	}
	t.Logf("catalog EXPLAIN: %s", rawPlan)
}

func TestCartProductsOnLiveDatabase(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var sku string
	if err := pool.QueryRow(ctx, `
		SELECT variant.sku FROM product_variants variant
		JOIN products product ON product.id=variant.product_id
		WHERE product.status='published' AND variant.is_active=1 AND variant.archived_at IS NULL
		ORDER BY variant.id LIMIT 1
	`).Scan(&sku); err != nil {
		t.Fatalf("select cart SKU: %v", err)
	}
	items, err := NewPostgresRepository(pool).CartProducts(ctx, []string{sku})
	if err != nil {
		t.Fatalf("load cart product: %v", err)
	}
	if len(items) != 1 || items[0].SKU != sku || !items[0].Available {
		t.Fatalf("unexpected cart product: %#v", items)
	}
}

func TestPublicStockUsesFreshSabyInventoryOnLiveDatabase(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	unique := time.Now().UnixNano()
	sabyID := fmt.Sprintf("catalog-stock-%d", unique)
	sku := fmt.Sprintf("9%d", unique)
	var categoryID, productID, variantID, warehouseID int64
	var productCode string
	if err := pool.QueryRow(ctx, `INSERT INTO categories(name,slug) VALUES($1,$2) RETURNING id`,
		"Stock guard fixture", fmt.Sprintf("stock-guard-%d", unique)).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM categories WHERE id=$1`, categoryID) })
	if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance) VALUES($1,$1,'Stock guard fixture',7)`, sabyID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO products(category_id,saby_id,name,slug,status,catalog_section,saby_fields)
		VALUES($1,$2,'Stock guard fixture',$3,'published','plants',ARRAY['stock']::TEXT[])
		RETURNING id,product_code::TEXT
	`, categoryID, sabyID, fmt.Sprintf("stock-guard-%d", unique)).Scan(&productID, &productCode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM products WHERE id=$1`, productID) })
	if err := pool.QueryRow(ctx, `
		INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor,is_active)
		VALUES($1,$2,$3,'D12',100000,1) RETURNING id
	`, productID, sabyID, sku).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM warehouses WHERE saby_id='saby-ryazan-main'`).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at)
		VALUES($1,$2,7,0,CURRENT_TIMESTAMP)
	`, warehouseID, variantID); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(pool)
	check := func(want int) {
		t.Helper()
		cart, err := repository.CartProducts(ctx, []string{sku})
		if err != nil || len(cart) != 1 || cart[0].Stock != want {
			t.Fatalf("cart stock=%v, err=%v; want %d", cart, err, want)
		}
		detail, err := repository.DetailBySlug(ctx, productCode)
		if err != nil || len(detail.Variants) != 1 || detail.Variants[0].Stock != want {
			t.Fatalf("detail stock=%v, err=%v; want %d", detail.Variants, err, want)
		}
		products, err := repository.ListAvailable(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, product := range products {
			if product.ID == productCode {
				found = true
				if product.Stock != want {
					t.Fatalf("catalog stock=%d; want %d", product.Stock, want)
				}
			}
		}
		if !found {
			t.Fatal("fixture product disappeared from catalog")
		}
		offers, err := repository.ListFeedOffers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found = false
		for _, offer := range offers {
			if offer.SKU == sku {
				found = true
				if offer.Stock != want {
					t.Fatalf("feed stock=%d; want %d", offer.Stock, want)
				}
			}
		}
		if !found {
			t.Fatal("fixture product disappeared from feed")
		}
		recommendations, err := repository.listRecommendations(ctx, 0, ProductDetail{CategoryID: &categoryID, CatalogSection: "plants"})
		if err != nil {
			t.Fatal(err)
		}
		found = false
		for _, recommendation := range recommendations {
			if recommendation.ID == productCode {
				found = true
				if recommendation.Stock != want {
					t.Fatalf("recommendation stock=%d; want %d", recommendation.Stock, want)
				}
			}
		}
		if !found {
			t.Fatal("fixture product disappeared from recommendations")
		}
	}
	check(7)
	if _, err := pool.Exec(ctx, `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP-INTERVAL '3 hours' WHERE variant_id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	check(0)
	if _, err := pool.Exec(ctx, `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP WHERE variant_id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE product_variants SET saby_id=NULL WHERE id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	check(0)
}

func TestLegacySabyProductCodeResolvesOnLiveDatabase(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	unique := time.Now().UnixNano()
	externalID := fmt.Sprintf("legacy-%d", unique)
	var productID, productCode int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO products(name,slug,status,catalog_section)
		VALUES($1,$2,'published','plants') RETURNING id,product_code
	`, "Legacy redirect fixture", fmt.Sprintf("legacy-redirect-%d", unique)).Scan(&productID, &productCode); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID) }()
	if _, err := pool.Exec(ctx, `INSERT INTO product_external_ids(product_id,provider,id_type,external_id) VALUES($1,'saby','id',$2)`, productID, externalID); err != nil {
		t.Fatal(err)
	}
	resolved, err := NewPostgresRepository(pool).resolveLegacyCode(ctx, "saby-"+externalID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != fmt.Sprint(productCode) {
		t.Fatalf("resolved=%q want=%d", resolved, productCode)
	}
}

func TestDocumentedLegacyURLAliasResolvesOnLiveDatabase(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var expected string
	if err := pool.QueryRow(ctx, `SELECT product.product_code::TEXT FROM product_url_aliases alias JOIN products product ON product.id=alias.product_id WHERE alias.alias='saby-3234'`).Scan(&expected); err != nil {
		t.Fatal(err)
	}
	resolved, err := NewPostgresRepository(pool).resolveLegacyCode(ctx, "saby-3234")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != expected {
		t.Fatalf("resolved=%q want=%q", resolved, expected)
	}
}
