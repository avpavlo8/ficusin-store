package catalog

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercise the queries used by the storefront, rather than a second Go
// implementation of the same ranking rules. Private empty tables prevent
// unrelated catalogue fixtures from affecting recommendation order or limits.
func TestCatalogRankingOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	schema := pgx.Identifier{fmt.Sprintf("catalog_ranking_%d", time.Now().UnixNano())}.Sanitize()
	if _, err = base.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer base.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(t *testing.T, query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{
		"categories", "products", "product_variants", "product_media", "media_mirror",
		"catalog_supplementary_media", "inventory", "warehouses", "saby_nomenclature",
		"launch_hidden_extra_plant_variants", "collections", "collection_products",
		"orders", "order_items", "product_reviews", "category_attributes", "attribute_definitions",
		"catalog_filters", "product_attribute_values", "variant_attribute_values", "attribute_options",
	} {
		exec(t, "CREATE TABLE "+pgx.Identifier{table}.Sanitize()+" (LIKE public."+pgx.Identifier{table}.Sanitize()+" INCLUDING ALL)")
	}
	repository := NewPostgresRepository(pool)
	seed := func(t *testing.T, id int64, name, section, kind, light string) {
		t.Helper()
		exec(t, `INSERT INTO products(id,product_code,name,slug,status,catalog_section,plant_kind,light_level,category_id)
		 VALUES($1::BIGINT,$1::INTEGER,$2,'ranking-'||$1::text,'published',$3,$4,$5,1)`, id, name, section, kind, light)
		exec(t, `INSERT INTO product_variants(id,product_id,sku,label,base_price_minor,is_active)
		 VALUES($1::BIGINT,$1::BIGINT,($1::BIGINT)::TEXT,'D12',10000,1)`, id)
	}
	exec(t, `INSERT INTO categories(id,name,slug) VALUES(1,'Ranking','ranking')`)
	t.Run("real sales and age buckets", func(t *testing.T) {
		for i, c := range []struct {
			days    int
			status  string
			payment string
			want    float64
		}{
			{1, "confirmed", "paid", 2},
			{60, "completed", "pending", 1},
			{200, "completed", "paid", .4},
			{500, "completed", "paid", .1},
			{1, "cancelled", "paid", 0},
			{1, "confirmed", "pending", 0},
		} {
			id := int64(i + 1)
			seed(t, id, fmt.Sprintf("Popularity %d", id), "plants", "", "")
			exec(t, `INSERT INTO orders(id,order_number,customer_name,phone,email,address,comment,delivery_method,delivery_fee,subtotal,total,status,payment_status,created_at)
			 VALUES($1::BIGINT,'RANK-'||($1::BIGINT)::TEXT,'Fixture','+79990000000','fixture@example.invalid','','','pickup',0,200,200,$2,$3,CURRENT_TIMESTAMP-$4::INTEGER*INTERVAL '1 day')`, id, c.status, c.payment, c.days)
			exec(t, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,unit_price,quantity)
			 VALUES($1::BIGINT,$1::BIGINT,$1::BIGINT,($1::BIGINT)::TEXT,'Fixture',100,2)`, id)
			items, err := repository.ListAvailable(ctx)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, item := range items {
				if item.ID == fmt.Sprint(id) {
					found = true
					if math.Abs(item.PopularityScore-c.want) > 1e-9 {
						t.Fatalf("case %d: score=%v want=%v", i, item.PopularityScore, c.want)
					}
				}
			}
			if !found {
				t.Fatalf("case %d: product absent from real catalogue query", i)
			}
		}
	})
	// Remove the popularity fixtures so the actual LIMIT 8 also belongs to the
	// recommendation scenario, rather than filtering results after the query.
	exec(t, "TRUNCATE products,product_variants,orders,order_items")
	seed(t, 11, "Z exact", "plants", "foliage", "low_light")
	seed(t, 12, "B section", "plants", "", "")
	seed(t, 13, "A other", "pots", "", "")
	t.Run("recommendation similarity", func(t *testing.T) {
		items, err := repository.listRecommendations(ctx, 0, ProductDetail{CatalogSection: "plants", PlantKind: "foliage", LightLevel: "low_light"})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 3 || items[0].ID != "11" || items[1].ID != "12" || items[2].ID != "13" {
			t.Fatalf("similarity order=%+v", items)
		}
	})
	t.Run("empty attributes do not prefer filled values", func(t *testing.T) {
		exec(t, "UPDATE products SET catalog_section='plants'")
		items, err := repository.listRecommendations(ctx, 0, ProductDetail{CatalogSection: "plants"})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 3 || items[0].ID != "13" || items[1].ID != "12" || items[2].ID != "11" {
			t.Fatalf("empty source attributes should leave name ordering: %+v", items)
		}
	})
}
