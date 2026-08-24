package catalog

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

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
