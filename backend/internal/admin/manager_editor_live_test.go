package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestManagerFullProductEditorOnLiveDatabase(t *testing.T) {
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
	tag := fmt.Sprint(time.Now().UnixNano())
	var customer int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(phone,full_name,consent_at) VALUES($1,'Full editor manager',NOW()) RETURNING id`, "full-editor-"+tag).Scan(&customer); err != nil {
		t.Fatal(err)
	}
	actor := Actor{CustomerID: customer, Role: RoleManager}
	repo := NewPostgresRepository(pool).WithEditorPublicationMode("mock")
	product, err := repo.CreateProduct(ctx, actor, ProductCreate{Name: "Manager full editor " + tag, CatalogSection: "plants", PriceMinor: 10000})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Exec(ctx, `DELETE FROM catalog_editor_price_publications WHERE product_id=$1`, product.ID)
		pool.Exec(ctx, `DELETE FROM catalog_editor_publications WHERE variant_id IN(SELECT id FROM product_variants WHERE product_id=$1)`, product.ID)
		pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, customer)
		pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, product.ID)
		pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customer)
	}()
	variants, err := repo.ListProductVariants(ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := variants[0].ID
	price := int64(12345)
	wholesale := 3
	label := "D13"
	newLabel := "D17"
	inactive := false
	_, err = repo.UpdateProduct(ctx, actor, product.ID, ProductUpdate{Variants: []VariantEdit{{ID: id, Label: &label, PriceMinor: &price, WholesaleMinQty: &wholesale}, {ID: -1, Label: &newLabel, PriceMinor: &price, Active: &inactive}}})
	if err != nil {
		t.Fatal(err)
	}
	variants, err = repo.ListProductVariants(ctx, product.ID)
	if err != nil || len(variants) != 2 {
		t.Fatalf("new variants: %v %v", variants, err)
	}
	var newID int64
	for _, v := range variants {
		if v.ID != id {
			newID = v.ID
		}
		if v.ID == id && (v.Price != float64(price)/100 || v.WholesaleMinQty != wholesale) {
			t.Fatalf("commercial fields not saved: %+v", v)
		}
	}
	// Commercial editor changes cannot change current inventory quantities.
	var warehouse int64
	if err = pool.QueryRow(ctx, `SELECT id FROM warehouses LIMIT 1`).Scan(&warehouse); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty) VALUES($1,$2,7,2) ON CONFLICT(warehouse_id,variant_id) DO UPDATE SET available_qty=7,reserved_qty=2`, warehouse, id); err != nil {
		t.Fatal(err)
	}
	_, err = repo.UpdateProduct(ctx, actor, product.ID, ProductUpdate{Variants: []VariantEdit{{ID: id, Label: &label, PriceMinor: &price, WholesaleMinQty: &wholesale, Active: &inactive}}})
	if err != nil {
		t.Fatal(err)
	}
	var available, reserved int
	if err = pool.QueryRow(ctx, `SELECT available_qty,reserved_qty FROM inventory WHERE warehouse_id=$1 AND variant_id=$2`, warehouse, id).Scan(&available, &reserved); err != nil || available != 7 || reserved != 2 {
		t.Fatalf("manager modified stock %d/%d %v", available, reserved, err)
	}
	if _, err = repo.ImportProducts(ctx, actor, ImportRequest{Codes: []string{"not-a-real-code"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("global import allowed: %v", err)
	}
	if err = repo.MergeDraftProducts(ctx, actor, MergeProductsRequest{TargetProductID: product.ID, SourceProductIDs: []int64{product.ID}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("bulk merge allowed: %v", err)
	}
	shared, err := repo.AddUploadedProductMedia(ctx, actor, product.ID, "https://example.test/shared.jpg", "https://example.test/shared.jpg", "https://example.test/shared.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.DeleteProductMedia(ctx, actor, product.ID, shared.ID); err != nil {
		t.Fatal(err)
	}
	photo, err := repo.AddUploadedVariantMedia(ctx, actor, id, "https://example.test/variant.jpg", "https://example.test/variant.jpg", "https://example.test/variant.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.DeleteVariantMedia(ctx, actor, id, photo.ID); err != nil {
		t.Fatal(err)
	}
	plan := EditorPlan{Variants: []EditorVariantPlan{{VariantID: id, Saby: EditorSaby{Mode: "create", Category: EditorCategory{ID: "fixture-category"}, Folder: EditorCategory{ID: "fixture-folder"}}}}}
	view, err := repo.SaveEditorPlan(ctx, actor, product.ID, plan, true)
	if err != nil || len(view.Publications) != 1 || view.Publications[0].Channel != "saby" || view.Publications[0].ExecutionMode != "mock" {
		t.Fatalf("manager cannot submit configured editor plan: %+v %v", view, err)
	}
	// The canonical atomic editor removal deletes free variants and archives
	// referenced ones; no alternate legacy variant DELETE API is required.
	if _, err = repo.UpdateProduct(ctx, actor, product.ID, ProductUpdate{Variants: []VariantEdit{{ID: newID, Remove: true}, {ID: id, Remove: true}}}); err != nil {
		t.Fatal(err)
	}
	var freeExists bool
	if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_variants WHERE id=$1)`, newID).Scan(&freeExists); err != nil || freeExists {
		t.Fatalf("free variant not deleted: %v", err)
	}
	var archived bool
	if err = pool.QueryRow(ctx, `SELECT archived_at IS NOT NULL FROM product_variants WHERE id=$1`, id).Scan(&archived); err != nil || !archived {
		t.Fatalf("stock-bearing variant not archived: %v", err)
	}
}
