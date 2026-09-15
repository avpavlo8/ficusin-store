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

func TestStage03CatalogueSafetyOnLiveDatabase(t *testing.T) {
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
	repository := NewPostgresRepository(pool)
	unique := time.Now().UnixNano()
	var staffID int64
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Stage 03 owner',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("stage-03-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&staffID); err != nil {
		t.Fatal(err)
	}
	owner := Actor{CustomerID: staffID, Role: RoleOwner}
	manager := Actor{CustomerID: staffID, Role: RoleManager}

	var productA, productB, variantA, variantB int64
	for index, target := range []*int64{&productA, &productB} {
		if err := pool.QueryRow(ctx, `INSERT INTO products(name,slug,status) VALUES($1,$2,'draft') RETURNING id`, fmt.Sprintf("Stage 03 product %d", index), fmt.Sprintf("stage-03-%d-%d", unique, index)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor,is_active) VALUES($1,'P12 H35',10000,1) RETURNING id`, productA).Scan(&variantA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor,is_active) VALUES($1,'P15 H35',12000,1) RETURNING id`, productB).Scan(&variantB); err != nil {
		t.Fatal(err)
	}

	t.Run("repeated Saby price import preserves local content", func(t *testing.T) {
		sabyID := fmt.Sprintf("stage-03-saby-%d", unique)
		if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,description,price_minor,balance) VALUES($1,$2,'Имя СБИС','Описание СБИС',15500,9)`, sabyID, fmt.Sprintf("X%d", unique)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE products SET saby_id=$2,description='Ручной контент' WHERE id=$1`, productA, sabyID); err != nil {
			t.Fatal(err)
		}
		for index := 0; index < 2; index++ {
			if _, err := repository.SyncProducts(ctx, owner, SyncRequest{ProductIDs: []int64{productA}, Fields: []string{"price"}}); err != nil {
				t.Fatal(err)
			}
		}
		var description string
		if err := pool.QueryRow(ctx, `SELECT description FROM products WHERE id=$1`, productA).Scan(&description); err != nil {
			t.Fatal(err)
		}
		if description != "Ручной контент" {
			t.Fatalf("Saby import overwrote content: %q", description)
		}
	})

	t.Run("external article conflict is rejected and relink keeps legacy history", func(t *testing.T) {
		oldID := fmt.Sprintf("OZON-%d-A", unique)
		newID := fmt.Sprintf("OZON-%d-B", unique)
		if _, err := pool.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,status,is_primary,source) VALUES($1,$2,'ozon','offer_id',$3,'active',TRUE,'manual')`, productA, variantA, oldID); err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = replaceVariantExternalIDs(ctx, tx, productB, variantB, []ExternalID{{Provider: "ozon", Type: "offer_id", ExternalID: oldID}})
		_ = tx.Rollback(ctx)
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("conflict=%v, want ErrInvalidInput", err)
		}
		tx, err = pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = replaceVariantExternalIDs(ctx, tx, productA, variantA, []ExternalID{{Provider: "ozon", Type: "offer_id", ExternalID: newID}}); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		var legacy, active int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FILTER(WHERE external_id=$2 AND status='legacy'),COUNT(*) FILTER(WHERE external_id=$3 AND status='active') FROM product_external_ids WHERE variant_id=$1`, variantA, oldID, newID).Scan(&legacy, &active); err != nil {
			t.Fatal(err)
		}
		if legacy != 1 || active != 1 {
			t.Fatalf("legacy=%d active=%d", legacy, active)
		}
	})

	t.Run("one product exposes two suppliers", func(t *testing.T) {
		var sabyID string
		if err := pool.QueryRow(ctx, `SELECT saby_id FROM products WHERE id=$1`, productA).Scan(&sabyID); err != nil {
			t.Fatal(err)
		}
		for index := 0; index < 2; index++ {
			var supplierID int64
			if err := pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,country_code,default_currency) VALUES($1,'domestic','RU','RUB') RETURNING id`, fmt.Sprintf("Stage 03 supplier %d %d", unique, index)).Scan(&supplierID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO procurement_supplier_products(supplier_id,saby_id,supplier_article,availability_status,canonical_variant_id) VALUES($1,$2,$3,'available',$4)`, supplierID, sabyID, fmt.Sprintf("SUP-%d", index), variantA); err != nil {
				t.Fatal(err)
			}
		}
		relations, err := repository.ProductRelationships(ctx, productA)
		if err != nil {
			t.Fatal(err)
		}
		if len(relations.Suppliers) != 2 {
			t.Fatalf("suppliers=%d, want 2", len(relations.Suppliers))
		}
	})

	t.Run("package dimensions accept decimals and reject zero", func(t *testing.T) {
		var categoryID, attributeID int64
		if err := pool.QueryRow(ctx, `INSERT INTO categories(name,slug) VALUES('Stage 03 packaging',$1) RETURNING id`, fmt.Sprintf("stage-03-packaging-%d", unique)).Scan(&categoryID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE products SET category_id=$2 WHERE id=$1`, productA, categoryID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO attribute_definitions(code,name,data_type,unit,audience,value_scope,is_active) VALUES($1,'Длина упаковки','number','см','technical','variant',TRUE) RETURNING id`, fmt.Sprintf("package_length_cm_%d", unique)).Scan(&attributeID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO category_attributes(category_id,attribute_id) VALUES($1,$2)`, categoryID, attributeID); err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		code := fmt.Sprintf("package_length_cm_%d", unique)
		if err = saveVariantPIMValues(ctx, tx, productA, variantA, map[string]any{code: 12.5}); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := validatePackageMeasurement("package_weight_grams", float64(0)); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("zero package weight accepted: %v", err)
		}
	})

	t.Run("pot change keeps product and historical order snapshot", func(t *testing.T) {
		var categoryID, attributeID, orderID int64
		if err := pool.QueryRow(ctx, `INSERT INTO categories(name,slug) VALUES('Stage 03 pots',$1) RETURNING id`, fmt.Sprintf("stage-03-pots-%d", unique)).Scan(&categoryID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE products SET category_id=$2 WHERE id=$1`, productB, categoryID); err != nil {
			t.Fatal(err)
		}
		code := fmt.Sprintf("pot_diameter_stage03_%d", unique)
		if err := pool.QueryRow(ctx, `INSERT INTO attribute_definitions(code,name,data_type,unit,audience,value_scope,is_active) VALUES($1,'Горшок','number','см','customer','variant',TRUE) RETURNING id`, code).Scan(&attributeID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO category_attributes(category_id,attribute_id) VALUES($1,$2)`, categoryID, attributeID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status) VALUES($1,'Stage 03','+70000000000','stage03@example.invalid','pickup',0,120,120,'completed') RETURNING id`, fmt.Sprintf("STAGE03-%d", unique)).Scan(&orderID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,unit_price,quantity) SELECT $1,$2,id,sku,'Историческое имя','P15 H35',120,1 FROM product_variants WHERE id=$3`, orderID, productB, variantB); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.UpdateVariantContent(ctx, manager, variantB, VariantContentUpdate{Attributes: map[string]any{code: float64(17.5)}}); err != nil {
			t.Fatal(err)
		}
		var products int
		var snapshot string
		if err := pool.QueryRow(ctx, `SELECT COUNT(*),(SELECT variant_label FROM order_items WHERE order_id=$2) FROM products WHERE id=$1`, productB, orderID).Scan(&products, &snapshot); err != nil {
			t.Fatal(err)
		}
		if products != 1 || snapshot != "P15 H35" {
			t.Fatalf("product=%d snapshot=%q", products, snapshot)
		}
		if _, err := repository.CreateCollectionDefinition(ctx, manager, CollectionDefinitionInput{Slug: fmt.Sprintf("pot-rule-%d", unique), Title: "По размеру горшка", CoverURL: "https://example.invalid/pot.jpg", Active: true, Mode: "dynamic", Rules: []CollectionRule{{Attribute: code, Operator: "gte", Value: 10}}}); err != nil {
			t.Fatal(err)
		}
		if err := repository.ArchiveAttributeDefinition(ctx, owner, attributeID); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("attribute used by collection was archived: %v", err)
		}
	})

	t.Run("collection requires cover and ordering is atomic", func(t *testing.T) {
		if _, err := repository.CreateCollectionDefinition(ctx, manager, CollectionDefinitionInput{Slug: fmt.Sprintf("empty-%d", unique), Title: "Без обложки", Mode: "manual"}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("empty cover accepted: %v", err)
		}
		first, err := repository.CreateCollectionDefinition(ctx, manager, CollectionDefinitionInput{Slug: fmt.Sprintf("first-%d", unique), Title: "Первая", CoverURL: "https://example.invalid/first.jpg", SortOrder: 10, Mode: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		second, err := repository.CreateCollectionDefinition(ctx, manager, CollectionDefinitionInput{Slug: fmt.Sprintf("second-%d", unique), Title: "Вторая", CoverURL: "https://example.invalid/second.jpg", SortOrder: 20, Mode: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		items, err := repository.ReorderCollectionDefinitions(ctx, manager, []int64{second.ID, first.ID})
		if err == nil {
			// The acceptance database may already contain seeded collections, so
			// a partial order must be rejected without touching either row.
			t.Fatalf("partial collection order unexpectedly accepted: %+v", items)
		}
		var firstOrder, secondOrder int
		if err := pool.QueryRow(ctx, `SELECT sort_order FROM collections WHERE id=$1`, first.ID).Scan(&firstOrder); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT sort_order FROM collections WHERE id=$1`, second.ID).Scan(&secondOrder); err != nil {
			t.Fatal(err)
		}
		if firstOrder != 10 || secondOrder != 20 {
			t.Fatalf("partial reorder changed rows: %d %d", firstOrder, secondOrder)
		}
	})

	if _, err := repository.UpdateProductVariant(ctx, manager, variantA, VariantInput{Label: "manager", PriceMinor: 1, Stock: 1, WholesaleMinQty: 1, Active: true}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager changed commercial fields: %v", err)
	}
}
