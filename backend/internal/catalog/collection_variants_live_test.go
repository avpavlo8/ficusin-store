package catalog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Conditions on different siblings must not manufacture a matching sale SKU.
func TestCollectionRequiresOneVisibleQualifyingVariantOnLiveDatabase(t *testing.T) {
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
	var productID, collectionID int64
	var code string
	if err := pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section) VALUES('Collection size fixture',$1,'published','plants') RETURNING id,product_code::text`, fmt.Sprintf("collection-size-%d", unique)).Scan(&productID, &code); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID) }()
	trait := fmt.Sprintf("size-fixture-%d", unique)
	if _, err := pool.Exec(ctx, `INSERT INTO product_attribute_values(product_id,attribute_id,value) SELECT $1,id,jsonb_build_array($2::text) FROM attribute_definitions WHERE code='storefront_traits'`, productID, trait); err != nil {
		t.Fatal(err)
	}
	rules := fmt.Sprintf(`[{"attribute":"storefront_traits","operator":"contains","value":%q},{"attribute":"height_cm","operator":"gte","value":80},{"attribute":"pot_diameter_cm","operator":"lte","value":20}]`, trait)
	slug := fmt.Sprintf("size-proof-%d", unique)
	if err := pool.QueryRow(ctx, `INSERT INTO collections(slug,title,cover_url,mode,rules,is_active,show_on_home) VALUES($1,'Size proof','/assets/test.webp','dynamic',$2::jsonb,1,FALSE) RETURNING id`, slug, rules).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM collections WHERE id=$1`, collectionID) }()
	ids := []int64{}
	skus := []string{}
	for index, size := range [][2]int{{35, 12}, {120, 30}, {150, 15}} {
		var id int64
		sku := fmt.Sprintf("6%016d%d", unique%10000000000000000, index)
		if err := pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active) VALUES($1,$2,$3,$4,1) RETURNING id`, productID, sku, fmt.Sprintf("D%d", size[1]), int64(100000+index*100000)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		skus = append(skus, sku)
		if _, err := pool.Exec(ctx, `INSERT INTO variant_attribute_values(variant_id,attribute_id,value) SELECT $1,id,CASE code WHEN 'height_cm' THEN to_jsonb($2::int) ELSE to_jsonb($3::int) END FROM attribute_definitions WHERE code IN ('height_cm','pot_diameter_cm')`, id, size[0], size[1]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO launch_hidden_extra_plant_variants(variant_id,product_id,excluded_sku) VALUES($1,$2,$3)`, ids[2], productID, skus[2]); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresRepository(pool)
	check := func(want bool) {
		t.Helper()
		cards, err := repo.ListForCollection(ctx, slug)
		if err != nil {
			t.Fatal(err)
		}
		if !want && len(cards) != 0 {
			t.Fatalf("different siblings/hidden SKU matched: %+v", cards)
		}
		if want && (len(cards) != 1 || cards[0].ID != code || cards[0].SKU != skus[2] || cards[0].Price != 3000 || cards[0].HeightCm == nil || *cards[0].HeightCm != 150) {
			t.Fatalf("wrong qualifying sale variant: %+v", cards)
		}
		collections, err := repo.ListCollections(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, collection := range collections {
			if collection.Slug == slug {
				found = true
				if collection.Count != 1 || collection.ShowOnHome || !collection.SelectsVariant {
					t.Fatalf("metadata/count mismatch: %+v", collection)
				}
			}
		}
		if found != want {
			t.Fatalf("collection presence %v, want %v", found, want)
		}
	}
	check(false)
	if _, err := pool.Exec(ctx, `DELETE FROM launch_hidden_extra_plant_variants WHERE variant_id=$1`, ids[2]); err != nil {
		t.Fatal(err)
	}
	check(true)
	if _, err := pool.Exec(ctx, `UPDATE product_variants SET archived_at=NOW() WHERE id=$1`, ids[2]); err != nil {
		t.Fatal(err)
	}
	check(false)
	if _, err := repo.ListForCollection(ctx, slug+"-missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing collection: %v", err)
	}
}
