package admin

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestVariantRemovalOnLiveDatabase(t *testing.T) {
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
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Removal test',NOW()) RETURNING id`, "removal-"+tag+"@example.invalid", "+7"+tag[len(tag)-10:]).Scan(&customer); err != nil {
		t.Fatal(err)
	}
	actor := Actor{CustomerID: customer, Role: RoleManager}
	repo := NewPostgresRepository(pool)
	p, err := repo.CreateProduct(ctx, actor, ProductCreate{Name: "Removal test " + tag, CatalogSection: "plants"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, customer)
		pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, p.ID)
		pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customer)
	}()
	for _, kind := range []string{"free", "stock", "negative-stock", "saby", "saby-external", "order", "history"} {
		t.Run(kind, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			var id int64
			must(tx.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,is_active) VALUES($1,'Removal size',1) RETURNING id`, p.ID).Scan(&id))
			switch kind {
			case "stock", "negative-stock":
				var w int64
				must(tx.QueryRow(ctx, `SELECT id FROM warehouses LIMIT 1`).Scan(&w))
				a, r := 0, 0
				if kind == "stock" {
					a = 3
				}
				if kind == "negative-stock" {
					a = -1
				}
				_, err = tx.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty) VALUES($1,$2,$3,$4)`, w, id, a, r)
				must(err)
			case "saby":
				_, err = tx.Exec(ctx, `UPDATE product_variants SET saby_id=$2 WHERE id=$1`, id, "remove-"+tag)
				must(err)
			case "saby-external":
				_, err = tx.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id) VALUES($1,$2,'saby','id',$3)`, p.ID, id, "remove-"+tag)
				must(err)
			case "order":
				var order int64
				must(tx.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status) VALUES($1,'Removal','+70000000000','','pickup',0,100,100,'new') RETURNING id`, "remove-"+tag).Scan(&order))
				_, err = tx.Exec(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,unit_price,quantity) VALUES($1,$2,$3,'removal','Removal','Size',100,1)`, order, p.ID, id)
				must(err)
			case "history":
				_, err = tx.Exec(ctx, `INSERT INTO stock_movements(variant_id,kind,quantity) VALUES($1,'test',1)`, id)
				must(err)
			}
			must(saveVariantEdits(ctx, tx, actor, p.ID, []VariantEdit{{ID: id, Remove: true}}))
			var archived bool
			err = tx.QueryRow(ctx, `SELECT archived_at IS NOT NULL AND is_active=0 FROM product_variants WHERE id=$1`, id).Scan(&archived)
			if kind == "free" {
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("free variant not deleted: %v", err)
				}
			} else {
				must(err)
				if !archived {
					t.Fatal("linked variant not archived")
				}
			}
		})
	}
	rows, err := repo.ListProductVariants(ctx, p.ID)
	if err != nil || len(rows) == 0 {
		t.Fatalf("variants: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE products SET status='published' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	edits := []VariantEdit{}
	for _, v := range rows {
		edits = append(edits, VariantEdit{ID: v.ID, Remove: true})
	}
	result, err := repo.UpdateProduct(ctx, actor, p.ID, ProductUpdate{Variants: edits})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "draft" {
		t.Fatal("last variant removal left card published")
	}
}
func TestVariantRemovalValidation(t *testing.T) {
	if validateVariantEdits(Actor{Role: RoleAdministrator}, []VariantEdit{{ID: 1, Remove: true}}) == nil {
		t.Fatal("administrator can delete")
	}
	label := "new"
	if validateVariantEdits(Actor{Role: RoleOwner}, []VariantEdit{{ID: 1, Remove: true, Label: &label}}) == nil {
		t.Fatal("combined delete/edit accepted")
	}
}
