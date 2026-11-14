package admin

import (
	"context"
	"errors"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestCanonicalPlantCollectionsOnLiveDatabase(t *testing.T) {
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
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	unique := time.Now().UnixNano()
	var actorID, productID, childID, plantsID, bonsaiID, variantID int64
	var productCode string
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Plant rules fixture',NOW()) RETURNING id`, fmt.Sprintf("plant-%d@example.invalid", unique), fmt.Sprintf("+798%09d", unique%1000000000)).Scan(&actorID))
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM admin_audit_log WHERE actor_customer_id=$1", actorID)
		_, _ = pool.Exec(ctx, "DELETE FROM customers WHERE id=$1", actorID)
	}()
	must(pool.QueryRow(ctx, "SELECT id FROM categories WHERE slug='plants'").Scan(&plantsID))
	// The production migrations configure collection rules, but a category may
	// be created later by the owner. The test owns its missing bonsai fixture.
	err = pool.QueryRow(ctx, "SELECT id FROM categories WHERE slug='bonsai'").Scan(&bonsaiID)
	if errors.Is(err, pgx.ErrNoRows) {
		must(pool.QueryRow(ctx, `INSERT INTO categories(parent_id,slug,name) VALUES($1,'bonsai','Bonsai fixture') RETURNING id`, plantsID).Scan(&bonsaiID))
		defer func() { _, _ = pool.Exec(ctx, "DELETE FROM categories WHERE id=$1", bonsaiID) }()
	} else {
		must(err)
	}
	must(pool.QueryRow(ctx, `INSERT INTO categories(parent_id,slug,name) VALUES($1,$2,'Bonsai child fixture') RETURNING id`, bonsaiID, fmt.Sprintf("bonsai-child-%d", unique)).Scan(&childID))
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM categories WHERE id=$1", childID) }()
	must(pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section,category_id) VALUES('Plant rules fixture',$1,'published','plants',$2) RETURNING id,product_code::text`, fmt.Sprintf("plant-rules-%d", unique), childID).Scan(&productID, &productCode))
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID) }()
	must(pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active) VALUES($1,$2,'D12',100000,1) RETURNING id`, productID, fmt.Sprintf("9%d", unique%10000000000000000)).Scan(&variantID))
	_, err = pool.Exec(ctx, `INSERT INTO variant_attribute_values(variant_id,attribute_id,value) SELECT $1,id,'28'::jsonb FROM attribute_definitions WHERE code='height_cm'`, variantID)
	must(err)
	repo := NewPostgresRepository(pool)
	actor := Actor{CustomerID: actorID, Role: RoleOwner}
	save := func(attrs map[string]any) {
		t.Helper()
		_, err := repo.UpdateProduct(ctx, actor, productID, ProductUpdate{Attributes: attrs})
		must(err)
	}
	match := func(slug string, want bool) {
		t.Helper()
		var got bool
		must(pool.QueryRow(ctx, `SELECT collection_rules_match_product($1,rules) FROM collections WHERE slug=$2`, productID, slug).Scan(&got))
		if got != want {
			t.Fatalf("%s got %v want %v", slug, got, want)
		}
	}
	save(map[string]any{"light_level": []string{"diffused", "low_light"}, "growth_habit": []string{"trailing", "climbing", "tree"}, "pet_safety": "safe", "care_level": "easy", "watering": "rare", "flowering": true, "fruiting": true, "leaf_coloring": "variegated", "wintering_type": "warm"})
	for _, slug := range []string{"dark", "pets", "easy", "rare", "flowering", "trailing", "vertical", "home-trees", "fruiting", "variegated", "bonsai", "indoor-bonsai", "compact"} {
		match(slug, true)
	}
	match("sunny-window", false)
	match("tall", false)
	save(map[string]any{"light_level": []string{"sunny"}, "wintering_type": "cold", "flowering": false, "fruiting": false, "leaf_coloring": "plain", "pet_safety": "unknown"})
	for _, slug := range []string{"dark", "pets", "flowering", "home-trees", "fruiting", "variegated", "indoor-bonsai"} {
		match(slug, false)
	}
	match("sunny-window", true)
	match("bonsai", true)
	save(map[string]any{"wintering_type": nil})
	match("indoor-bonsai", false)
	_, err = repo.UpdateProduct(ctx, actor, productID, ProductUpdate{CategoryID: &plantsID})
	must(err)
	match("bonsai", false)
	save(map[string]any{"wintering_type": "warm"})
	match("indoor-bonsai", false)
	for _, slug := range []string{"bathroom", "office", "bedroom"} {
		var id int64
		var mode string
		must(pool.QueryRow(ctx, "SELECT id,mode FROM collections WHERE slug=$1", slug).Scan(&id, &mode))
		if mode != "manual" {
			t.Fatalf("%s is %s", slug, mode)
		}
		must(repo.SetProductCollectionMembership(ctx, actor, productID, id, true))
		var included bool
		must(pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM collection_products WHERE collection_id=$1 AND product_id=$2)", id, productID).Scan(&included))
		if !included {
			t.Fatal("manual membership not saved")
		}
		must(repo.SetProductCollectionMembership(ctx, actor, productID, id, false))
	}
	cards, err := catalog.NewPostgresRepository(pool).ListForCollection(ctx, "sunny-window")
	must(err)
	found := false
	for _, card := range cards {
		if card.ID == productCode {
			found = true
		}
	}
	if !found {
		t.Fatal("public collection omitted matching product")
	}
}
