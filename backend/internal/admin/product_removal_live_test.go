package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProductRemovalOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || (!strings.HasSuffix(cfg.ConnConfig.Database, "_test") && cfg.ConnConfig.Database != "ficusin_ci") {
		t.Fatal("requires isolated loopback test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tag := fmt.Sprint(time.Now().UnixNano())
	var customer int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(phone,full_name,consent_at) VALUES($1,'Removal test',NOW()) RETURNING id`, tag).Scan(&customer); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customer)
	repo := NewPostgresRepository(pool)
	actor := Actor{CustomerID: customer, Role: RoleOwner}
	for _, kind := range []string{"free", "stock", "reserved", "negative-stock", "saby", "wb", "history", "order", "published"} {
		t.Run(kind, func(t *testing.T) {
			creationID := ""
			if kind == "free" {
				creationID = "00000000-0000-4000-8000-" + tag[len(tag)-12:]
			}
			p, err := repo.CreateProduct(ctx, actor, ProductCreate{CreationID: creationID, Name: "Product removal " + tag + kind, CatalogSection: "plants", PriceMinor: 10000})
			if err != nil {
				t.Fatal(err)
			}
			rows, err := repo.ListProductVariants(ctx, p.ID)
			if err != nil || len(rows) != 1 {
				t.Fatal(err, rows)
			}
			v := rows[0].ID
			var order, reservation int64
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			exec := func(q string, args ...any) { t.Helper(); _, err := pool.Exec(ctx, q, args...); must(err) }
			defer func() {
				pool.Exec(ctx, `DELETE FROM cashier_stock_layers WHERE reservation_id=$1`, reservation)
				pool.Exec(ctx, `DELETE FROM cashier_stock_reservations WHERE id=$1`, reservation)
				pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, order)
				pool.Exec(ctx, `DELETE FROM stock_movements WHERE variant_id=$1`, v)
				pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, p.ID)
				pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, customer)
			}()
			exec(`INSERT INTO product_media(product_id,object_key,is_primary) VALUES($1,'https://example.invalid/removal.jpg',1)`, p.ID)
			switch kind {
			case "stock", "negative-stock":
				var wh int64
				must(pool.QueryRow(ctx, `SELECT id FROM warehouses LIMIT 1`).Scan(&wh))
				a, r := 0, 0
				if kind == "stock" {
					a = 2
				}
				if kind == "negative-stock" {
					a = -1
				}
				exec(`INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty) VALUES($1,$2,$3,$4) ON CONFLICT(warehouse_id,variant_id) DO UPDATE SET available_qty=$3,reserved_qty=$4`, wh, v, a, r)
			case "reserved":
				var wh int64
				must(pool.QueryRow(ctx, `SELECT id FROM warehouses LIMIT 1`).Scan(&wh))
				must(pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status,payment_method) VALUES($1,'Removal','+70000000000','','pickup',0,100,100,'new','on_delivery') RETURNING id`, "product-reserved-"+tag).Scan(&order))
				must(pool.QueryRow(ctx, `INSERT INTO cashier_stock_reservations(order_id,warehouse_id) VALUES($1,$2) RETURNING id`, order, wh).Scan(&reservation))
				exec(`INSERT INTO cashier_stock_layers(reservation_id,variant_id,quantity) VALUES($1,$2,1)`, reservation, v)
			case "saby":
				exec(`UPDATE product_variants SET saby_id='removal-'||id::text WHERE id=$1`, v)
			case "wb":
				exec(`INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id) VALUES($1,$2,'wb','nm_id',$3)`, p.ID, v, tag)
			case "history":
				exec(`INSERT INTO stock_movements(variant_id,kind,quantity) VALUES($1,'test',1)`, v)
			case "order":
				must(pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status) VALUES($1,'Removal','+70000000000','','pickup',0,100,100,'new') RETURNING id`, "product-removal-"+tag).Scan(&order))
				exec(`INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,unit_price,quantity) VALUES($1,$2,$3,'removal','Snapshot name','Size',100,1)`, order, p.ID, v)
			case "published":
				exec(`UPDATE products SET status='published' WHERE id=$1`, p.ID)
			}
			if kind != "free" && kind != "published" {
				if _, err = repo.DeleteDraftProducts(ctx, actor, []int64{p.ID}); !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("bulk delete bypassed references: %v", err)
				}
			}
			result, err := repo.RemoveProduct(ctx, actor, p.ID)
			must(err)
			if kind == "free" {
				if result.Action != "deleted" {
					t.Fatal(result)
				}
				var exists bool
				must(pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id=$1)`, p.ID).Scan(&exists))
				if exists {
					t.Fatal("free draft retained")
				}
				var tombstone bool
				must(pool.QueryRow(ctx, `SELECT product_id IS NULL FROM catalog_product_creation_requests WHERE actor_customer_id=$1 AND creation_id=$2`, customer, creationID).Scan(&tombstone))
				if !tombstone {
					t.Fatal("creation tombstone lost")
				}
				if _, err = repo.CreateProduct(ctx, actor, ProductCreate{CreationID: creationID, Name: "Must not recreate", CatalogSection: "plants"}); !errors.Is(err, ErrInvalidInput) {
					t.Fatal("deleted draft recreated", err)
				}
				return
			}
			if result.Action != "archived" {
				t.Fatal(result)
			}
			var status string
			must(pool.QueryRow(ctx, `SELECT status FROM products WHERE id=$1`, p.ID).Scan(&status))
			if status != "archived" {
				t.Fatal(status)
			}
			media, err := repo.ListProductMedia(ctx, p.ID)
			must(err)
			if len(media) != 1 || !strings.Contains(media[0].URL, "removal.jpg") {
				t.Fatal("archive lost photo", media)
			}
			variants, err := repo.ListProductVariants(ctx, p.ID)
			must(err)
			if len(variants) != 1 || variants[0].ID != v {
				t.Fatal("archive changed variants")
			}
			if kind == "order" {
				orders, err := repo.ListOrders(ctx)
				must(err)
				found := false
				for _, o := range orders {
					if o.ID != order {
						continue
					}
					found = true
					if len(o.Items) != 1 || o.Items[0].ProductID != p.ID || o.Items[0].ProductCode != "" || o.Items[0].Image == "" || o.Items[0].ProductName != "Snapshot name" {
						t.Fatal("order lost archived product/photo or retained dead PDP link", o.Items)
					}
				}
				if !found {
					t.Fatal("order missing")
				}
			}
			repeated, err := repo.RemoveProduct(ctx, actor, p.ID)
			must(err)
			if repeated.Action != "archived" {
				t.Fatal("archive not idempotent")
			}
		})
	}
	if _, err = repo.RemoveProduct(ctx, Actor{Role: RoleManager}, 1); !errors.Is(err, ErrForbidden) {
		t.Fatal("manager delete permission widened", err)
	}
	if _, err = repo.RemoveProduct(ctx, actor, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err = repo.RemoveProduct(ctx, actor, 9223372036854775807); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
}
