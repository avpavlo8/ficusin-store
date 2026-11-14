package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCategoryCRUDPreservesHierarchyDeletionGuardsAndPublicIconOnLiveDatabase(t *testing.T) {
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
	repo := NewPostgresRepository(pool)
	actor := Actor{Role: RoleOwner}
	tag := fmt.Sprint(time.Now().UnixNano())
	var parentID, childID, productID int64
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		_, _ = pool.Exec(ctx, `DELETE FROM categories WHERE id=$1`, childID)
		_, _ = pool.Exec(ctx, `DELETE FROM categories WHERE id=$1`, parentID)
	}()
	parent, err := repo.CreateCategory(ctx, actor, CategoryCreate{Name: "Category parent", Slug: "category-parent-" + tag})
	if err != nil {
		t.Fatal(err)
	}
	parentID = parent.ID
	child, err := repo.CreateCategory(ctx, actor, CategoryCreate{ParentID: &parentID, Name: "Category child", Slug: "category-child-" + tag})
	if err != nil {
		t.Fatal(err)
	}
	childID = child.ID
	name, sortOrder := "Updated child", 73
	child, err = repo.UpdateCategory(ctx, actor, childID, CategoryUpdate{Name: &name, SortOrder: &sortOrder})
	if err != nil || child.Name != name || child.SortOrder != sortOrder || child.ParentID == nil || *child.ParentID != parentID || child.ProductsCount != 0 {
		t.Fatalf("category CRUD lost current fields/hierarchy: %+v %v", child, err)
	}
	if err = repo.DeleteCategory(ctx, actor, parentID); !errors.Is(err, ErrCategoryNotEmpty) {
		t.Fatalf("deleted category with children: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE categories SET icon='plant-test-icon' WHERE id=$1`, parentID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,category_id) VALUES('Category product',$1,'published',$2) RETURNING id`, "category-product-"+tag, childID).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor,is_active) VALUES($1,'Current SKU',10000,1)`, productID); err != nil {
		t.Fatal(err)
	}
	categories, err := repo.ListCategories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range categories {
		if item.ID == childID {
			found = true
			if item.ProductsCount != 1 || item.ParentID == nil || *item.ParentID != parentID {
				t.Fatalf("admin projection lost live counts/hierarchy: %+v", item)
			}
		}
	}
	if !found {
		t.Fatal("new category missing from list")
	}
	if err = repo.DeleteCategory(ctx, actor, childID); !errors.Is(err, ErrCategoryNotEmpty) {
		t.Fatalf("deleted category with a product: %v", err)
	}
	public, err := catalog.NewPostgresRepository(pool).ListCategories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, item := range public {
		if item.ID == parentID {
			found = true
			if item.Icon != "plant-test-icon" {
				t.Fatalf("admin response cleanup changed public category icon: %+v", item)
			}
		}
	}
	if !found {
		t.Fatal("occupied category ancestors missing from storefront")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID); err != nil {
		t.Fatal(err)
	}
	if err = repo.DeleteCategory(ctx, actor, childID); err != nil {
		t.Fatalf("empty leaf cannot be deleted: %v", err)
	}
	if err = repo.DeleteCategory(ctx, actor, parentID); err != nil {
		t.Fatalf("empty parent cannot be deleted: %v", err)
	}
}
