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

func TestCatalogReadinessUsesCurrentVariantSchemaOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var category, product, active, archived, attribute, unused int64
	t.Cleanup(func() {
		for _, row := range []struct {
			query string
			id    int64
		}{
			{"DELETE FROM products WHERE id=$1", product},
			{"DELETE FROM categories WHERE id=$1", category},
			{"DELETE FROM attribute_definitions WHERE id=$1", attribute},
			{"DELETE FROM attribute_definitions WHERE id=$1", unused},
		} {
			if _, err := pool.Exec(ctx, row.query, row.id); err != nil {
				t.Error(err)
			}
		}
	})
	insert := func(id *int64, query string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, query, args...).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	tag := fmt.Sprintf("readiness-test-%d", time.Now().UnixNano())
	insert(&category, `INSERT INTO categories(name,slug) VALUES('Readiness test',$1) RETURNING id`, tag)
	insert(&product, `INSERT INTO products(name,slug,category_id) VALUES('Readiness test',$1,$2) RETURNING id`, tag, category)
	insert(&active, `INSERT INTO product_variants(product_id,label,sku,base_price_minor,is_active) VALUES($1,'D12','998940012',10000,1) RETURNING id`, product)
	insert(&archived, `INSERT INTO product_variants(product_id,label,sku,base_price_minor,is_active,archived_at) VALUES($1,'D15','998940015',10000,0,now()) RETURNING id`, product)
	insert(&attribute, `INSERT INTO attribute_definitions(code,name,data_type,value_scope) VALUES('test_readiness_fit','Тип горшка','enum','variant') RETURNING id`)
	insert(&unused, `INSERT INTO attribute_definitions(code,name,data_type,value_scope) VALUES('test_readiness_old','Старый атрибут','enum','variant') RETURNING id`)
	exec(`INSERT INTO category_attributes(category_id,attribute_id) VALUES($1,$2)`, category, attribute)
	exec(`INSERT INTO attribute_options(attribute_id,code,label) VALUES($1,'current','Текущий')`, attribute)
	exec(`INSERT INTO variant_attribute_values(variant_id,attribute_id,value) VALUES($1,$3,'"current"'),($2,$3,'"old"'),($1,$4,'"hidden"')`, active, archived, attribute, unused)
	repo := NewPostgresRepository(pool)
	find := func() Product {
		t.Helper()
		items, err := repo.ListProducts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.ID == product {
				return item
			}
		}
		t.Fatal("product missing from catalogue")
		return Product{}
	}
	if item := find(); item.UnknownEnumValues || len(item.VariantEnumTasks) != 0 {
		t.Fatalf("obsolete fields raised a warning: %v", item.VariantEnumTasks)
	}
	exec(`UPDATE variant_attribute_values SET value='"retired"' WHERE variant_id=$1 AND attribute_id=$2`, active, attribute)
	if item := find(); !item.UnknownEnumValues || len(item.VariantEnumTasks) != 1 || !strings.Contains(item.VariantEnumTasks[0], "D12 · артикул 998940012") || !strings.Contains(item.VariantEnumTasks[0], "Тип горшка") {
		t.Fatalf("active invalid value lost its variant or attribute: %v", item.VariantEnumTasks)
	}
}
