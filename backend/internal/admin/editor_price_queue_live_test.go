package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercise the actual DB approval/lease path and the same HTTP emulator and
// adapter dispatch used by the local application. No vendor network is possible.
func TestEditorPriceQueueLocalDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var name string
	if err = pool.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "ficusin_local_publisher_test" {
		t.Skip("isolated publisher database required")
	}
	if err = migrate.Apply(ctx, pool, "../../../timeweb/migrations"); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	tag := fmt.Sprint(time.Now().UnixNano())
	var actorID int64
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Price adapter fixture',NOW()) RETURNING id`, tag+"@example.invalid", "+7"+tag[len(tag)-10:]).Scan(&actorID))
	actor := Actor{CustomerID: actorID, Role: RoleOwner}
	repo := NewPostgresRepository(pool).WithEditorPublicationMode("mock")
	defer pool.Exec(ctx, "DELETE FROM customers WHERE id=$1", actorID)
	for _, ch := range []string{"wb", "ozon"} {
		t.Run(ch, func(t *testing.T) {
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			product, err := repo.CreateProduct(ctx, actor, ProductCreate{Name: "Price adapter " + tag + ch, PriceMinor: 10000, CatalogSection: "plants"})
			must(err)
			variants, err := repo.ListProductVariants(ctx, product.ID)
			must(err)
			variant := variants[0].ID
			defer func() {
				pool.Exec(ctx, "DELETE FROM catalog_editor_price_publications WHERE variant_id=$1", variant)
				pool.Exec(ctx, "DELETE FROM catalog_editor_remote_links WHERE variant_id=$1", variant)
				pool.Exec(ctx, "DELETE FROM admin_audit_log WHERE actor_customer_id=$1", actorID)
				pool.Exec(ctx, "DELETE FROM products WHERE id=$1", product.ID)
			}()
			cards := integration.NewMockCatalog()
			request := ce.Request{Name: "Queue fixture", Description: "Queue fixture", Barcode: "2000000000015", Images: []string{"https://images.example.test/queue.jpg"}, Packaging: ce.Packaging{Length: 30, Width: 20, Height: 45, Weight: 1250}, Marketplace: ce.Marketplace{Article: "queue-" + tag + ch, Brand: "Фикусин", Model: "Plant", Units: 1, TNVED: "0602909900"}, WBPrice: "4000", OzonPrice: "5000"}
			request.Channel = ce.Channel{Enabled: true, Mode: "create", Category: ce.Category{ID: "900001", TypeID: 900002}}
			request.Marketplace.Ozon = request.Channel
			request.Schema = []ce.Attribute{{ID: 4191, Name: "Аннотация"}}
			if ch == "wb" {
				request.Channel.Category = ce.Category{ID: "900003"}
				request.Schema = []ce.Attribute{{CharcID: 15000001, Name: "Страна", CharcType: 1}}
				request.Channel.Attributes = map[string]any{"15000001": "Россия"}
			}
			progress := ce.Progress{}
			for i := 0; i < 20; i++ {
				res, err := cards.Advance(ctx, ch, request, progress)
				must(err)
				progress = res.Progress
				if res.Status == "created" {
					break
				}
			}
			if progress.IDs.ProductID == "" {
				t.Fatal("no local card")
			}
			_, err = pool.Exec(ctx, `INSERT INTO catalog_editor_remote_links(variant_id,channel,execution_mode,remote_id,offer_id,sku,barcode) VALUES($1,$2,'mock',$3,$4,$5,$6)`, variant, ch, progress.IDs.ProductID, progress.IDs.OfferID, progress.IDs.SKU, progress.IDs.Barcode)
			must(err)
			target := progress.IDs.ProductID
			if ch == "ozon" {
				target = progress.IDs.OfferID
			}
			compare := int64(200000)
			command := ce.Request{Action: "price", ProductID: product.ID, VariantID: variant, PriceMinor: 10000, Price: &ce.PriceRequest{TargetID: target, PriceMinor: 150000, CompareAtMinor: &compare}}
			raw, err := json.Marshal(command)
			must(err)
			var id int64
			must(pool.QueryRow(ctx, `INSERT INTO catalog_editor_price_publications(product_id,variant_id,channel,target_id,expected_site_price_minor,price_minor,compare_at_minor,payload,fingerprint,price_origin,created_by) VALUES($1,$2,$3,$4,10000,150000,200000,$5,encode(sha256(convert_to($5::jsonb::text,'UTF8')),'hex'),'explicit',$6) RETURNING id`, product.ID, variant, ch, target, raw, actorID).Scan(&id))
			prices, err := repo.EditorPrices(ctx, product.ID)
			must(err)
			if len(prices) != 1 || !prices[0].CanApprove {
				t.Fatalf("unapprovable draft %+v", prices)
			}
			publisher := &integration.EditorPublisher{Cards: cards, Prices: &integration.EditorPricePublisher{Marketplaces: cards.Executor, Mock: true}}
			must(ce.PricePublicationStep(ctx, pool, publisher, "mock"))
			var status string
			must(pool.QueryRow(ctx, "SELECT status FROM catalog_editor_price_publications WHERE id=$1", id).Scan(&status))
			if status != "draft" {
				t.Fatal("unapproved draft executed")
			}
			_, err = repo.ApproveEditorPrice(ctx, actor, product.ID, id, prices[0].Fingerprint)
			must(err)
			for i := 0; i < 5; i++ {
				_, err = pool.Exec(ctx, "UPDATE catalog_editor_price_publications SET next_attempt_at=NOW() WHERE id=$1", id)
				must(err)
				must(ce.PricePublicationStep(ctx, pool, publisher, "mock"))
				must(pool.QueryRow(ctx, "SELECT status FROM catalog_editor_price_publications WHERE id=$1", id).Scan(&status))
				if status == "synced" {
					break
				}
			}
			if status != "synced" {
				t.Fatalf("real outbox + adapter flow did not finish: %s", status)
			}
			result, err := publisher.Advance(ctx, ch, command, ce.Progress{Stage: "price-verify"})
			must(err)
			if result.Status != "synced" {
				t.Fatalf("remote tuple not confirmed: %+v", result)
			}
			var count int
			must(pool.QueryRow(ctx, "SELECT count(*) FROM product_external_ids WHERE variant_id=$1", variant).Scan(&count))
			if count != 0 {
				t.Fatal("price lane leaked mock IDs into canonical catalogue")
			}
		})
	}
}
