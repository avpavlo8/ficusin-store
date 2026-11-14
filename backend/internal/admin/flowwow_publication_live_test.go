package admin

import (
	"context"
	"fmt"
	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	fw "github.com/avpavlo8/ficusin-store/backend/internal/flowwow"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestFlowwowPublicationLocalDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	var db string
	if e = pool.QueryRow(ctx, `SELECT current_database()`).Scan(&db); e != nil || db != "ficusin_local_publisher_test" {
		t.Fatal("isolated database required", e)
	}
	if e = migrate.Apply(ctx, pool, "../../../timeweb/migrations"); e != nil {
		t.Fatal(e)
	}
	tag := fmt.Sprint(time.Now().UnixNano())
	var customer int64
	if e = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Flowwow test',NOW()) RETURNING id`, tag+"@example.invalid", "+7"+tag[len(tag)-10:]).Scan(&customer); e != nil {
		t.Fatal(e)
	}
	actor := Actor{CustomerID: customer, Role: RoleOwner}
	repo := NewPostgresRepository(pool).WithEditorPublicationMode("mock")
	p, e := repo.CreateProduct(ctx, actor, ProductCreate{Name: "Flowwow site " + tag, Description: "Site description", PriceMinor: 123400, CatalogSection: "plants", VariantAttributes: map[string]any{"pot_diameter_cm": 13, "height_cm": 30}})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		pool.Exec(ctx, `DELETE FROM catalog_editor_remote_links WHERE variant_id IN(SELECT id FROM product_variants WHERE product_id=$1)`, p.ID)
		pool.Exec(ctx, `DELETE FROM catalog_editor_publications WHERE variant_id IN(SELECT id FROM product_variants WHERE product_id=$1)`, p.ID)
		pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, customer)
		pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, p.ID)
		pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customer)
	}()
	variants, e := repo.ListProductVariants(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	id := variants[0].ID
	if _, e = pool.Exec(ctx, `INSERT INTO product_media(product_id,object_key,is_primary) VALUES($1,'https://images.example.test/flowwow.jpg',1)`, p.ID); e != nil {
		t.Fatal(e)
	}
	plan := EditorPlan{Variants: []EditorVariantPlan{{VariantID: id, Saby: EditorSaby{Mode: "create", Category: EditorCategory{ID: "local"}, Folder: EditorCategory{ID: "local"}}, Marketplace: EditorMarketplace{Name: "Must not replace site name", Description: "Must not replace site description"}, Flowwow: EditorChannel{Enabled: true, Mode: "create", Category: EditorCategory{ID: "101"}, Attributes: map[string]any{"stock": 3, "height": map[string]any{"val": 30, "measure": 3}}}}}}
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, plan, true); e != nil {
		t.Fatal(e)
	}
	client := integration.NewMockFlowwowClient()
	publisher := ce.ChannelPublishers{"flowwow": client}
	for i := 0; i < 4; i++ {
		if e = ce.PublicationStep(ctx, pool, publisher, "mock"); e != nil {
			t.Fatal(e)
		}
	}
	var status, remote, offer string
	if e = pool.QueryRow(ctx, `SELECT status FROM catalog_editor_publications WHERE variant_id=$1 AND channel='flowwow'`, id).Scan(&status); e != nil || status != "synced" {
		t.Fatalf("status %s %v", status, e)
	}
	if e = pool.QueryRow(ctx, `SELECT remote_id,offer_id FROM catalog_editor_remote_links WHERE variant_id=$1 AND channel='flowwow' AND execution_mode='mock'`, id).Scan(&remote, &offer); e != nil {
		t.Fatal(e)
	}
	items, _, e := client.Products(ctx, map[string]any{"offerIds": []string{offer}})
	if e != nil || len(items) != 1 || items[0].Name != p.Name || items[0].Description != "Site description" {
		t.Fatalf("site authority %+v %v", items, e)
	}
	// Established links keep updating even if the create checkbox is cleared.
	plan.Variants[0].Flowwow.Enabled = false
	if _, e = pool.Exec(ctx, `UPDATE product_variants SET base_price_minor=234567 WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, plan, true); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 4; i++ {
		if e = ce.PublicationStep(ctx, pool, publisher, "mock"); e != nil {
			t.Fatal(e)
		}
	}
	items, _, e = client.Products(ctx, map[string]any{"offerIds": []string{offer}})
	if e != nil || len(items) != 1 || fmt.Sprint(items[0].ProductID) != remote || items[0].Price != "2345.67" {
		t.Fatalf("linked update %+v %v", items, e)
	}

	// Content-only administrators reuse the stored Flowwow connection after the
	// main branch's permission split, without changing channel identity or price.
	if _, e = pool.Exec(ctx, `UPDATE products SET description='Administrator saved content' WHERE id=$1`, p.ID); e != nil {
		t.Fatal(e)
	}
	syncResult, e := repo.SyncExistingEditorPlan(ctx, Actor{CustomerID: customer, Role: RoleAdministrator}, p.ID)
	if e != nil || syncResult.Queued != 1 {
		t.Fatalf("stored Flowwow sync %+v %v", syncResult, e)
	}
	for i := 0; i < 3; i++ {
		if e = ce.PublicationStep(ctx, pool, publisher, "mock"); e != nil {
			t.Fatal(e)
		}
	}
	items, _, e = client.Products(ctx, map[string]any{"offerIds": []string{offer}})
	if e != nil || len(items) != 1 || items[0].Description != "Administrator saved content" || fmt.Sprint(items[0].ProductID) != remote {
		t.Fatalf("stored content sync %+v %v", items, e)
	}
	var leaked int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM product_external_ids WHERE variant_id=$1 AND provider IN ('flowwow','wildberries','ozon')`, id).Scan(&leaked); e != nil || leaked != 0 {
		t.Fatal("mock IDs leaked", leaked, e)
	}

	// Exercise the inventory worker with a network-isolated API adapter and live
	// receipt namespace. A stale Saby snapshot must send zero, not its old balance.
	sabyID := "flowwow-test-" + tag
	if _, e = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,name) VALUES($1,'Test source')`, sabyID); e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID)
	if _, e = pool.Exec(ctx, `UPDATE products SET status='published',saby_fields=ARRAY['stock'] WHERE id=$1`, p.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `UPDATE product_variants SET saby_id=$2,is_active=1 WHERE id=$1`, id, sabyID); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO inventory(variant_id,warehouse_id,available_qty,reserved_qty,synced_at) SELECT $1,id,5,0,NOW() FROM warehouses WHERE saby_id='saby-ryazan-main' ON CONFLICT(warehouse_id,variant_id) DO UPDATE SET available_qty=5,reserved_qty=0,synced_at=NOW()`, id); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO catalog_editor_remote_links(variant_id,channel,execution_mode,remote_id,offer_id) VALUES($1,'flowwow','live',$2,$3)`, id, remote, offer); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `UPDATE catalog_editor_publications SET execution_mode='live' WHERE variant_id=$1 AND channel='flowwow'`, id); e != nil {
		t.Fatal(e)
	}
	service := &fw.Service{Pool: pool, Client: client, Mode: "live"}
	if e = service.InventoryStep(ctx); e != nil {
		t.Fatal(e)
	}
	items, _, e = client.Products(ctx, map[string]any{"offerIds": []string{offer}})
	if e != nil || len(items) != 1 || items[0].Stock != 5 {
		t.Fatalf("fresh stock %+v %v", items, e)
	}
	if _, e = pool.Exec(ctx, `UPDATE inventory SET synced_at=NOW()-INTERVAL '3 hours' WHERE variant_id=$1`, id); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `UPDATE product_variants SET base_price_minor=345678 WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	if e = service.InventoryStep(ctx); e != nil {
		t.Fatal(e)
	}
	items, _, e = client.Products(ctx, map[string]any{"offerIds": []string{offer}})
	if e != nil || len(items) != 1 || items[0].Stock != 0 || items[0].Price != "3456.78" {
		t.Fatalf("stale stock/price %+v %v", items, e)
	}
	if e = pool.QueryRow(ctx, `SELECT last_error FROM flowwow_inventory_sync WHERE variant_id=$1`, id).Scan(&status); e != nil || status != "" {
		t.Fatal("inventory failed", status, e)
	}
	if _, e = pool.Exec(ctx, `DELETE FROM catalog_editor_remote_links WHERE variant_id=$1 AND execution_mode='live'`, id); e != nil {
		t.Fatal(e)
	}
	repo.WithEditorPublicationMode("disabled")
	plan.Variants[0].Flowwow.Enabled = true
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, plan, true); e != nil {
		t.Fatal(e)
	}
	if e = ce.PublicationStep(ctx, pool, publisher, "live"); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `SELECT status FROM catalog_editor_publications WHERE variant_id=$1 AND channel='flowwow'`, id).Scan(&status); e != nil || status != "prepared" {
		t.Fatal("disabled preparation was promoted", status, e)
	}
}
