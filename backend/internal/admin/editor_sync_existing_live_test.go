package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestManagerExistingEditorSyncOnLiveDatabase(t *testing.T) {
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
	if err = pool.QueryRow(ctx, `INSERT INTO customers(phone,full_name,consent_at) VALUES($1,'Editor sync test',NOW()) RETURNING id`, "editor-sync-"+tag).Scan(&customer); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresRepository(pool).WithEditorPublicationMode("mock")
	owner := Actor{CustomerID: customer, Role: RoleOwner}
	manager := Actor{CustomerID: customer, Role: RoleManager}
	product, err := repo.CreateProduct(ctx, owner, ProductCreate{Name: "Editor sync " + tag, Description: "Saved description", PriceMinor: 10000, CatalogSection: "plants"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Exec(ctx, `DELETE FROM catalog_editor_remote_links WHERE variant_id IN(SELECT id FROM product_variants WHERE product_id=$1)`, product.ID)
		pool.Exec(ctx, `DELETE FROM catalog_editor_publications WHERE variant_id IN(SELECT id FROM product_variants WHERE product_id=$1)`, product.ID)
		pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, customer)
		pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, product.ID)
		pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customer)
	}()
	variants, err := repo.ListProductVariants(ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	variant := variants[0].ID
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	if _, err = repo.SyncExistingEditorPlan(ctx, Actor{Role: "unknown"}, product.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("domain permission bypass", err)
	}
	exec(`INSERT INTO catalog_editor_plans(product_id,config) VALUES($1,'{"unsubmittedOwnerDraft":"must-not-publish"}')`, product.ID)
	remote := "sync-" + tag
	exec(`INSERT INTO catalog_editor_remote_links(variant_id,channel,execution_mode,remote_id,offer_id) VALUES($1,'wb','mock',$2,$2),($1,'ozon','mock',$3,$3)`, variant, remote, remote+"-ozon")
	result, err := repo.SyncExistingEditorPlan(ctx, manager, product.ID)
	if err != nil || result.Queued != 0 || result.Skipped != 2 || len(result.Warnings) != 2 {
		t.Fatalf("missing configuration must be visible: %+v %v", result, err)
	}
	prior := ce.Request{ProductID: product.ID, VariantID: variant, Action: "update", ExistingID: remote, Name: "Old site name", Description: "Old site description", PriceMinor: 10000, SKU: "protected-sku", Barcode: "2000000000008", OzonPrice: "999", WBPrice: "888", Marketplace: ce.Marketplace{Name: "Old site name", Description: "Custom vendor copy", Brand: "Protected brand", TNVED: "0602909900"}, Channel: ce.Channel{Category: ce.Category{ID: "protected-category"}}, Images: []string{"https://example.test/old.jpg"}}
	raw, _ := json.Marshal(prior)
	exec(`INSERT INTO catalog_editor_publications(variant_id,channel,fingerprint,payload,status,execution_mode) VALUES($1,'wb',$2,$3,'synced','mock')`, variant, editorPublicationFingerprint("wb", raw), raw)
	exec(`INSERT INTO product_media(product_id,variant_id,object_key,is_primary) VALUES($1,NULL,'https://example.test/new.jpg',1),($1,$2,'https://example.test/video.mp4?signature=not-an-image',0)`, product.ID, variant)
	for code, n := range map[string]int{"package_length_cm": 31, "package_width_cm": 21, "package_height_cm": 41, "package_weight_grams": 1250, "pot_diameter_cm": 13} {
		exec(`INSERT INTO variant_attribute_values(variant_id,attribute_id,value) SELECT $1,id,to_jsonb($3::int) FROM attribute_definitions WHERE code=$2 ON CONFLICT(variant_id,attribute_id) DO UPDATE SET value=EXCLUDED.value`, variant, code, n)
	}
	// Protected live price and an unsubmitted owner draft are not publisher inputs.
	exec(`UPDATE product_variants SET base_price_minor=77777 WHERE id=$1`, variant)
	result, err = repo.SyncExistingEditorPlan(ctx, manager, product.ID)
	if err != nil || result.Queued != 1 || result.Skipped != 1 {
		t.Fatalf("sync: %+v %v", result, err)
	}
	var updated ce.Request
	var status string
	var generated *string
	var config map[string]any
	if err = pool.QueryRow(ctx, `SELECT payload,status FROM catalog_editor_publications WHERE variant_id=$1 AND channel='wb'`, variant).Scan(&updated, &status); err != nil {
		t.Fatal(err)
	}
	if status != "prepared" || updated.Action != "update" || updated.ExistingID != remote || updated.Name != product.Name || updated.Description != "Saved description" || updated.PriceMinor != 10000 || updated.WBPrice != "888" || updated.OzonPrice != "999" || updated.Channel.Category.ID != "protected-category" || updated.Marketplace.Brand != "Protected brand" || updated.Marketplace.Description != "Custom vendor copy" || updated.Marketplace.Name != product.Name || !reflect.DeepEqual(updated.Images, []string{"https://example.test/new.jpg"}) || updated.Packaging.Weight != 1250 || updated.Packaging.Season == "" {
		t.Fatalf("unsafe content snapshot: %+v status=%s", updated, status)
	}
	pool.QueryRow(ctx, `SELECT generated_barcode FROM product_variants WHERE id=$1`, variant).Scan(&generated)
	pool.QueryRow(ctx, `SELECT config FROM catalog_editor_plans WHERE product_id=$1`, product.ID).Scan(&config)
	if generated != nil || config["unsubmittedOwnerDraft"] != "must-not-publish" {
		t.Fatal("manager changed barcode or owner plan")
	}
	result, err = repo.SyncExistingEditorPlan(ctx, manager, product.ID)
	if err != nil || result.Queued != 0 {
		t.Fatalf("unchanged save queued again: %+v %v", result, err)
	}
	// An in-flight update retains every receipt and gets only a pending snapshot.
	exec(`UPDATE catalog_editor_publications SET status='processing',progress='{"stage":"verify","ids":{"productId":"receipt"}}' WHERE variant_id=$1 AND channel='wb'`, variant)
	exec(`UPDATE products SET description='Newer content' WHERE id=$1`, product.ID)
	result, err = repo.SyncExistingEditorPlan(ctx, manager, product.ID)
	if err != nil || result.Queued != 1 {
		t.Fatalf("pending snapshot: %+v %v", result, err)
	}
	var pending ce.Request
	var progress ce.Progress
	if err = pool.QueryRow(ctx, `SELECT payload,pending_payload,progress,status FROM catalog_editor_publications WHERE variant_id=$1 AND channel='wb'`, variant).Scan(&updated, &pending, &progress, &status); err != nil {
		t.Fatal(err)
	}
	if status != "processing" || updated.Description != "Saved description" || pending.Description != "Newer content" || progress.Stage != "verify" || progress.IDs.ProductID != "receipt" {
		t.Fatal("in-flight receipt overwritten")
	}
	// Unknown outcomes and owner relink plans are never restarted by this path.
	exec(`UPDATE catalog_editor_publications SET status='failed',progress='{"ambiguous":true,"createAttempted":true}' WHERE variant_id=$1 AND channel='wb'`, variant)
	result, err = repo.SyncExistingEditorPlan(ctx, manager, product.ID)
	if err != nil || result.Queued != 0 || result.Skipped != 2 {
		t.Fatalf("unknown write restarted: %+v %v", result, err)
	}
	// A completed creation can only become an update of its exact confirmed card.
	prior.Action = "create"
	raw, _ = json.Marshal(prior)
	ids, _ := json.Marshal(ce.Progress{IDs: ce.RemoteIDs{ProductID: remote}})
	exec(`UPDATE catalog_editor_publications SET status='created',payload=$2,progress=$3,pending_payload=NULL,pending_fingerprint=NULL WHERE variant_id=$1 AND channel='wb'`, variant, raw, ids)
	result, err = repo.SyncExistingEditorPlan(ctx, manager, product.ID)
	if err != nil || result.Queued != 1 {
		t.Fatalf("completed creation did not update linked card: %+v %v", result, err)
	}
	exec(`UPDATE catalog_editor_publications SET payload=jsonb_set(payload,'{variantId}',to_jsonb(($2::bigint))) WHERE variant_id=$1 AND channel='wb'`, variant, variant+1)
	result, err = repo.SyncExistingEditorPlan(ctx, manager, product.ID)
	if err != nil || result.Queued != 0 || result.Skipped != 2 {
		t.Fatalf("foreign snapshot accepted: %+v %v", result, err)
	}
	// Saby uses the canonical numeric identity, its UUID proof, and the pot
	// suffix while retaining category/folder and price from the active plan.
	sabyID := "editor-sync-saby-" + tag
	exec(`UPDATE product_variants SET saby_id=$2 WHERE id=$1`, variant, sabyID)
	prior.Action, prior.ExistingID, prior.SabyID = "update", sabyID, sabyID
	prior.SabyUUID = "1a034658-824a-4000-8000-000000000001"
	prior.Saby = ce.Saby{Category: ce.Category{ID: "1041"}, Folder: ce.Category{ID: "3333"}}
	raw, _ = json.Marshal(prior)
	exec(`INSERT INTO catalog_editor_publications(variant_id,channel,fingerprint,payload,status,execution_mode) VALUES($1,'saby',$2,$3,'synced','mock')`, variant, editorPublicationFingerprint("saby", raw), raw)
	result, err = repo.SyncExistingEditorPlan(ctx, manager, product.ID)
	if err != nil || result.Queued != 1 {
		t.Fatalf("Saby content sync: %+v %v", result, err)
	}
	if err = pool.QueryRow(ctx, `SELECT payload FROM catalog_editor_publications WHERE variant_id=$1 AND channel='saby'`, variant).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Action != "update" || updated.ExistingID != sabyID || updated.SabyID != sabyID || updated.SabyUUID != prior.SabyUUID || updated.Name != product.Name+" D13" || updated.Description != "Newer content" || updated.Saby.Category.ID != "1041" || updated.Saby.Folder.ID != "3333" || updated.PriceMinor != 10000 {
		t.Fatalf("unsafe Saby update: %+v", updated)
	}
	exec(`UPDATE product_variants SET archived_at=NOW() WHERE id=$1`, variant)
	result, err = repo.SyncExistingEditorPlan(ctx, manager, product.ID)
	if err != nil || result.Queued != 0 || result.Skipped != 0 || len(result.Warnings) != 0 {
		t.Fatalf("archived variant/no-op must not queue or warn: %+v %v", result, err)
	}
}
