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

func TestCatalogEditorAtomicSaveOnLiveDatabase(t *testing.T) {
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
	var actorID, categoryID int64
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Catalog editor test',NOW()) RETURNING id`, fmt.Sprintf("editor-%d@example.invalid", unique), fmt.Sprintf("+799%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM categories WHERE slug='plants'`).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	actor := Actor{CustomerID: actorID, Role: RoleOwner}
	repository := NewPostgresRepository(pool)
	product, err := repository.CreateProduct(ctx, actor, ProductCreate{Name: fmt.Sprintf("Editor draft %d", unique), CategoryID: &categoryID, CatalogSection: "plants"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_action_batches WHERE manual_variant_id IN(SELECT id FROM product_variants WHERE product_id=$1)`, product.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, actorID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, product.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID)
	}()
	if product.Status != "draft" {
		t.Fatalf("new product must be hidden: %s", product.Status)
	}
	var publicCode string
	if err := pool.QueryRow(ctx, `SELECT product_code::TEXT FROM products WHERE id=$1`, product.ID).Scan(&publicCode); err != nil {
		t.Fatal(err)
	}
	if product.ProductCode != publicCode {
		t.Fatalf("admin storefront URL must use canonical product code, got %q want %q", product.ProductCode, publicCode)
	}
	variants, err := repository.ListProductVariants(ctx, product.ID)
	if err != nil || len(variants) != 1 {
		t.Fatalf("initial variants %v: %v", variants, err)
	}
	first := variants[0].ID
	inactive := false
	label := "D12"
	price := int64(123400)
	secondLabel := "D17"
	secondPrice := int64(234500)
	// A new size is a local draft without copied stock or identities.
	_, err = repository.UpdateProduct(ctx, actor, product.ID, ProductUpdate{Variants: []VariantEdit{{ID: first, Label: &label, PriceMinor: &price, Active: &inactive}, {ID: -1, Label: &secondLabel, PriceMinor: &secondPrice, Active: &inactive}}})
	if err != nil {
		t.Fatal(err)
	}
	variants, err = repository.ListProductVariants(ctx, product.ID)
	if err != nil || len(variants) != 2 {
		t.Fatalf("saved variants %v: %v", variants, err)
	}
	second := variants[1].ID
	if variants[1].Stock != 0 || len(variants[1].ExternalIDs) != 0 {
		t.Fatal("new size copied stock or identifiers")
	}
	var warehouseID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM warehouses ORDER BY id LIMIT 1`).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at) VALUES($1,$2,11,0,NOW()) ON CONFLICT(warehouse_id,variant_id) DO UPDATE SET available_qty=11`, warehouseID, first); err != nil {
		t.Fatal(err)
	}
	name := "Must roll back"
	changedPrice := int64(999900)
	_, err = repository.UpdateProduct(ctx, actor, product.ID, ProductUpdate{Name: &name, Variants: []VariantEdit{{ID: first, PriceMinor: &changedPrice, Active: &inactive}, {ID: second, Attributes: map[string]any{"package_weight_grams": float64(-1)}}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid packaging: %v", err)
	}
	actual, err := repository.productByID(ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Name != product.Name {
		t.Fatal("product content committed before rejected size")
	}
	var storedPrice int64
	var stock int
	if err := pool.QueryRow(ctx, `SELECT base_price_minor FROM product_variants WHERE id=$1`, first).Scan(&storedPrice); err != nil {
		t.Fatal(err)
	}
	if storedPrice != price {
		t.Fatal("price was partially committed")
	}
	name = "Saved whole card"
	_, err = repository.UpdateProduct(ctx, actor, product.ID, ProductUpdate{Name: &name, Variants: []VariantEdit{{ID: first, PriceMinor: &changedPrice, Active: &inactive}, {ID: second, Attributes: map[string]any{"package_weight_grams": float64(1250)}}}})
	if err != nil {
		t.Fatal(err)
	}
	actual, err = repository.productByID(ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Name != name {
		t.Fatal("content not saved")
	}
	variants, err = repository.ListProductVariants(ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if variants[0].Price != 9999 || variants[1].Attributes["package_weight_grams"] != float64(1250) {
		t.Fatalf("reload lost saved sizes: %+v", variants)
	}
	if err := pool.QueryRow(ctx, `SELECT available_qty FROM inventory WHERE warehouse_id=$1 AND variant_id=$2`, warehouseID, first).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if stock != 11 {
		t.Fatal("content save overwrote inventory")
	}
	published := "published"
	_, err = repository.UpdateProduct(ctx, actor, product.ID, ProductUpdate{Status: &published})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("individual publication without active sizes: %v", err)
	}
	result, err := repository.PublishDraftProducts(ctx, actor, []int64{product.ID})
	if err != nil || len(result.Published) != 0 || len(result.Blocked) != 1 {
		t.Fatalf("bulk publication differs: %+v %v", result, err)
	}
	actual, err = repository.productByID(ctx, product.ID)
	if err != nil || actual.Status != "draft" {
		t.Fatalf("rejected publication changed status: %+v %v", actual, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE products SET status='published' WHERE id=$1`, product.ID); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	active := true
	_, err = repository.UpdateProduct(ctx, actor, product.ID, ProductUpdate{Variants: []VariantEdit{{ID: first, PriceMinor: &zero, Active: &active}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero-price active published size accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE products SET status='draft' WHERE id=$1`, product.ID); err != nil {
		t.Fatal(err)
	}
	foreignID := int64(999999999999)
	_, err = repository.UpdateProduct(ctx, actor, product.ID, ProductUpdate{Variants: []VariantEdit{{ID: foreignID, Label: &label}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("foreign size: %v", err)
	}
}

func TestCatalogEditorManagerCannotWriteCommercialFields(t *testing.T) {
	price := int64(0)
	active := false
	actor := Actor{Role: RoleManager}
	for _, edit := range []VariantEdit{{ID: 1, PriceMinor: &price}, {ID: 1, Active: &active}, {ID: -1}} {
		if err := ValidateProductUpdate(actor, ProductUpdate{Variants: []VariantEdit{edit}}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("manager commercial edit allowed: %v", err)
		}
	}
	label := "D12"
	if err := ValidateProductUpdate(actor, ProductUpdate{Variants: []VariantEdit{{ID: 1, Label: &label}}}); err != nil {
		t.Fatal(err)
	}
	fields := []string{"price"}
	if err := ValidateProductUpdate(Actor{Role: RoleOwner}, ProductUpdate{SabyFields: &fields}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("site price can be replaced by Saby")
	}
}
