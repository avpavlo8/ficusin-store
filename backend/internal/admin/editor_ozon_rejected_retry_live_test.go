package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOzonRejectedSavePreservesCreationReceiptOnLocalDatabase(t *testing.T) {
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
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	var db string
	must(pool.QueryRow(ctx, `SELECT current_database()`).Scan(&db))
	if db != "ficusin_local_publisher_test" {
		t.Skip("requires isolated publisher fixture database")
	}
	must(migrate.Apply(ctx, pool, "../../../timeweb/migrations"))
	tag := fmt.Sprint(time.Now().UnixNano())
	var actorID, productID, variantID int64
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Ozon retry fixture',NOW()) RETURNING id`, tag+"@example.invalid", "+7"+tag[len(tag)-10:]).Scan(&actorID))
	defer func() {
		pool.Exec(ctx, `DELETE FROM catalog_editor_remote_links WHERE variant_id=$1`, variantID)
		pool.Exec(ctx, `DELETE FROM catalog_editor_publications WHERE variant_id=$1`, variantID)
		pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, actorID)
		pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID)
	}()
	actor := Actor{CustomerID: actorID, Role: RoleOwner}
	repo := NewPostgresRepository(pool).WithEditorPublicationMode("mock")
	product, err := repo.CreateProduct(ctx, actor, ProductCreate{Name: "Ozon retry " + tag, Description: "Описание", PriceMinor: 10000, CatalogSection: "plants", VariantAttributes: map[string]any{"pot_diameter_cm": 13, "height_cm": 28}})
	must(err)
	productID = product.ID
	variants, err := repo.ListProductVariants(ctx, productID)
	must(err)
	if len(variants) != 1 {
		t.Fatal("fixture variant missing")
	}
	variantID = variants[0].ID
	for code, value := range map[string]int{"package_length_cm": 40, "package_width_cm": 20, "package_height_cm": 20, "package_winter_length_cm": 40, "package_winter_width_cm": 20, "package_winter_height_cm": 20, "package_weight_grams": 1} {
		_, err = pool.Exec(ctx, `INSERT INTO variant_attribute_values(variant_id,attribute_id,value) SELECT $1,id,to_jsonb($3::int) FROM attribute_definitions WHERE code=$2 ON CONFLICT(variant_id,attribute_id) DO UPDATE SET value=EXCLUDED.value`, variantID, code, value)
		must(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO product_media(product_id,object_key,is_primary) VALUES($1,'https://images.example.test/fixture.jpg',1)`, productID)
	must(err)
	mock := integration.NewMockCatalog()
	schema, err := mock.Executor.ReadEditorReference(ctx, "ozon", "attributes", 900001, 900002, 0, "")
	must(err)
	must(repo.CacheEditorReference(ctx, "ozon", "attributes", "900001:900002:0:", schema))
	for _, attribute := range []int64{85, 22232, 90002} {
		values, err := mock.Executor.ReadEditorReference(ctx, "ozon", "values", 900001, 900002, attribute, "")
		must(err)
		must(repo.CacheEditorReference(ctx, "ozon", "values", fmt.Sprintf("900001:900002:%d:", attribute), values))
	}
	marketplace := EditorMarketplace{Name: "Ozon retry fixture", Description: "Описание", Article: "retry-" + tag, Brand: "Фикусин", BrandID: 900101, TNVED: "0602909900", TNVEDID: 900102, Model: "Растение", Units: 1,
		Ozon: EditorChannel{Enabled: true, Mode: "create", Category: EditorCategory{ID: "900001", TypeID: 900002}, Attributes: map[string]any{"90002": []ce.Value{{Value: "Россия", DictionaryID: 900103}}}},
	}
	plan := EditorPlan{Variants: []EditorVariantPlan{{VariantID: variantID, OzonPrice: "1000", Saby: EditorSaby{Mode: "create", Category: EditorCategory{ID: "local"}, Folder: EditorCategory{ID: "local"}}, Marketplace: marketplace}}}
	_, err = repo.SaveEditorPlan(ctx, actor, productID, plan, true)
	must(err)
	var oldRequest ce.Request
	must(pool.QueryRow(ctx, `SELECT payload FROM catalog_editor_publications WHERE variant_id=$1 AND channel='ozon'`, variantID).Scan(&oldRequest))
	if oldRequest.Packaging.Weight != 1 || oldRequest.Barcode == "" {
		t.Fatal("fixture did not freeze original one-gram request")
	}
	proof, _ := json.Marshal(ce.Progress{Stage: "ozon-poll", TaskID: 123, CreateAttempted: true, UpdateRequest: &oldRequest})
	_, err = pool.Exec(ctx, `UPDATE catalog_editor_publications SET status='failed',progress=$2 WHERE variant_id=$1 AND channel='ozon'`, variantID, proof)
	must(err)
	_, err = pool.Exec(ctx, `UPDATE variant_attribute_values SET value='1000'::jsonb WHERE variant_id=$1 AND attribute_id=(SELECT id FROM attribute_definitions WHERE code='package_weight_grams')`, variantID)
	must(err)
	_, err = repo.SaveEditorPlan(ctx, actor, productID, plan, true)
	must(err)
	var current ce.Request
	var progress ce.Progress
	var status string
	must(pool.QueryRow(ctx, `SELECT payload,progress,status FROM catalog_editor_publications WHERE variant_id=$1 AND channel='ozon'`, variantID).Scan(&current, &progress, &status))
	if status != "prepared" || current.Packaging.Weight != 1000 || current.Barcode != oldRequest.Barcode || !progress.CreateAttempted || !progress.ReapplyPayload || progress.Stage != "reconcile" || progress.TaskID != 123 || progress.UpdateRequest != nil {
		t.Fatalf("corrected Save lost creation provenance or kept old content: %s %+v %+v", status, current, progress)
	}
	calls := 0
	boundary := editorPublisherFunc(func(_ context.Context, ch string, r ce.Request, p ce.Progress) (ce.Result, error) {
		calls++
		if ch != "ozon" || r.Packaging.Weight != 1000 || !p.ReapplyPayload || !p.CreateAttempted || p.Stage != "reconcile" {
			t.Error("worker did not reconcile corrected immutable request")
		}
		return ce.Result{Status: "failed", Progress: p, Message: "isolated receipt boundary"}, nil
	})
	must(ce.PublicationStep(ctx, pool, boundary, "mock"))
	if calls != 1 {
		t.Fatal("worker did not consume exactly the corrected request")
	}
	must(repo.RetryEditorPublication(ctx, actor, productID, variantID, "ozon"))
	must(pool.QueryRow(ctx, `SELECT progress FROM catalog_editor_publications WHERE variant_id=$1 AND channel='ozon'`, variantID).Scan(&progress))
	if !progress.ReapplyPayload || progress.Stage != "reconcile" {
		t.Fatal("explicit retry resumed rejected task without corrected import")
	}
	// Changing seller identity is a new intent, never a way to transfer the
	// previous attempt's proof onto an arbitrary existing seller article.
	_, err = pool.Exec(ctx, `UPDATE catalog_editor_publications SET status='failed' WHERE variant_id=$1 AND channel='ozon'`, variantID)
	must(err)
	plan.Variants[0].Marketplace.Article = "another-" + tag
	_, err = repo.SaveEditorPlan(ctx, actor, productID, plan, true)
	must(err)
	must(pool.QueryRow(ctx, `SELECT progress FROM catalog_editor_publications WHERE variant_id=$1 AND channel='ozon'`, variantID).Scan(&progress))
	if progress.CreateAttempted || progress.ReapplyPayload {
		t.Fatal("creation provenance transferred to a different seller article")
	}
	// A process can die after sending the corrected import but before saving
	// its task ID. Lease recovery must not re-enter the update POST stage.
	crashProof, _ := json.Marshal(ce.Progress{Stage: "update", ReapplyPayload: true, CreateAttempted: true})
	_, err = pool.Exec(ctx, `UPDATE catalog_editor_publications SET progress=$2,status='processing',lease_until=NOW()-INTERVAL '1 second' WHERE variant_id=$1 AND channel='ozon'`, variantID, crashProof)
	must(err)
	recovered := false
	readOnlyBoundary := editorPublisherFunc(func(_ context.Context, ch string, _ ce.Request, p ce.Progress) (ce.Result, error) {
		recovered = true
		if ch != "ozon" || p.Stage != "ozon-reapply-unknown" || !p.Ambiguous {
			t.Error("expired corrected import would repeat its POST")
		}
		return ce.Result{Status: "needs_attention", Progress: p}, nil
	})
	must(ce.PublicationStep(ctx, pool, readOnlyBoundary, "mock"))
	if !recovered {
		t.Fatal("expired corrected import was not recovered")
	}
	// A read error in the unknown-update boundary may set failed, but neither
	// another Save nor a pending revision can turn ownership into proof that
	// the previous corrected import finished.
	unknownUpdate := ce.Progress{Stage: "ozon-reapply-unknown", ReapplyPayload: true, CreateAttempted: true, Ambiguous: true, TaskID: 123, IDs: ce.RemoteIDs{ProductID: "990000111", OfferID: plan.Variants[0].Marketplace.Article}}
	unknownUpdateRaw, _ := json.Marshal(unknownUpdate)
	_, err = pool.Exec(ctx, `UPDATE catalog_editor_publications SET progress=$2,status='failed' WHERE variant_id=$1 AND channel='ozon'`, variantID, unknownUpdateRaw)
	must(err)
	plan.Variants[0].OzonPrice = "1200"
	_, err = repo.SaveEditorPlan(ctx, actor, productID, plan, true)
	must(err)
	var pendingUnknown bool
	must(pool.QueryRow(ctx, `SELECT status,progress,pending_payload IS NOT NULL FROM catalog_editor_publications WHERE variant_id=$1 AND channel='ozon'`, variantID).Scan(&status, &progress, &pendingUnknown))
	if status != "failed" || !pendingUnknown || progress.Stage != "ozon-reapply-unknown" || !progress.Ambiguous || progress.IDs != unknownUpdate.IDs || progress.TaskID != 123 {
		t.Fatal("changed Save discarded the unknown corrected import")
	}
	must(repo.RetryEditorPublication(ctx, actor, productID, variantID, "ozon"))
	unknownReads := 0
	failedReadBoundary := editorPublisherFunc(func(_ context.Context, ch string, _ ce.Request, p ce.Progress) (ce.Result, error) {
		unknownReads++
		if ch != "ozon" || p.Stage != "ozon-reapply-unknown" || !p.Ambiguous {
			t.Fatalf("retry would repost an unknown corrected import: %+v", p)
		}
		return ce.Result{Progress: p}, errors.New("isolated unknown-import read failure")
	})
	must(ce.PublicationStep(ctx, pool, failedReadBoundary, "mock"))
	must(pool.QueryRow(ctx, `SELECT status,progress,pending_payload IS NOT NULL FROM catalog_editor_publications WHERE variant_id=$1 AND channel='ozon'`, variantID).Scan(&status, &progress, &pendingUnknown))
	if unknownReads != 1 || status != "failed" || !pendingUnknown || progress.Stage != "ozon-reapply-unknown" || !progress.Ambiguous || progress.IDs != unknownUpdate.IDs {
		t.Fatal("a failed read promoted pending content over an unknown import")
	}
	// A later Save must not erase an accepted-but-unconfirmed WB upload either.
	wbSchema, err := mock.Executor.ReadEditorReference(ctx, "wb", "attributes", 900003, 0, 0, "")
	must(err)
	must(repo.CacheEditorReference(ctx, "wb", "attributes", "900003:0:0:", wbSchema))
	plan.Variants[0].Marketplace.WB = EditorChannel{Enabled: true, Mode: "create", Category: EditorCategory{ID: "900003"}, Attributes: map[string]any{"15000001": "Россия"}}
	plan.Variants[0].WBPrice = "1000"
	_, err = repo.SaveEditorPlan(ctx, actor, productID, plan, true)
	must(err)
	wbUnknown, _ := json.Marshal(ce.Progress{Stage: "reconcile", CreateAttempted: true})
	_, err = pool.Exec(ctx, `UPDATE catalog_editor_publications SET progress=$2,status='failed' WHERE variant_id=$1 AND channel='wb'`, variantID, wbUnknown)
	must(err)
	plan.Variants[0].WBPrice = "1100"
	_, err = repo.SaveEditorPlan(ctx, actor, productID, plan, true)
	must(err)
	var pending bool
	must(pool.QueryRow(ctx, `SELECT payload,progress,pending_payload IS NOT NULL FROM catalog_editor_publications WHERE variant_id=$1 AND channel='wb'`, variantID).Scan(&current, &progress, &pending))
	if !pending || !progress.CreateAttempted || progress.Stage != "reconcile" || current.WBPrice != "1000" {
		t.Fatal("changed Save erased an unconfirmed WB upload and enabled another Create")
	}
}
