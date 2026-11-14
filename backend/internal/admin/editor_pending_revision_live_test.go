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

func TestPendingWBRevisionRunsAfterDefinitivePreflightFailureOnLocalDatabase(t *testing.T) {
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
	var product, variant int64
	must(pool.QueryRow(ctx, `INSERT INTO products(name,slug,status) VALUES('Pending revision fixture',$1,'draft') RETURNING id`, "pending-revision-"+tag).Scan(&product))
	defer func() {
		pool.Exec(ctx, `DELETE FROM catalog_editor_remote_links WHERE variant_id=$1`, variant)
		pool.Exec(ctx, `DELETE FROM catalog_editor_publications WHERE variant_id=$1`, variant)
		pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, product)
	}()
	must(pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor) VALUES($1,'fixture',10000) RETURNING id`, product).Scan(&variant))
	old := ce.Request{ProductID: product, VariantID: variant, Action: "create", Name: "Растение", Description: "Описание", Barcode: "2000000000015", Images: []string{"https://images.example.test/fixture.jpg"}, WBPrice: "1500", Packaging: ce.Packaging{Length: 40, Width: 20, Height: 20, Weight: 1000},
		Marketplace: ce.Marketplace{Article: "pending-wb-" + tag, Brand: "Фикусин", TNVED: "invalid"}, Channel: ce.Channel{Enabled: true, Mode: "create", Category: ce.Category{ID: "900003"}}, Schema: []ce.Attribute{{CharcID: 15000003, Name: "Код ТН ВЭД", Required: true, CharcType: 1, MaxCount: 1}}}
	next := old
	next.Marketplace.TNVED = "0601109000"
	oldRaw, _ := json.Marshal(old)
	nextRaw, _ := json.Marshal(next)
	_, err = pool.Exec(ctx, `INSERT INTO catalog_editor_publications(variant_id,channel,fingerprint,payload,status,execution_mode,progress,pending_payload,pending_fingerprint)
	 VALUES($1,'wb','old',$2,'prepared','mock','{"stage":"create"}',$3,'new')`, variant, oldRaw, nextRaw)
	must(err)
	mock := integration.NewMockCatalog()
	// The old payload fails before upload. A saved, valid newer snapshot must
	// become the active request even though no remote product ID exists yet.
	must(ce.PublicationStep(ctx, pool, mock, "mock"))
	var current ce.Request
	var progress ce.Progress
	var status string
	var pending bool
	must(pool.QueryRow(ctx, `SELECT payload,progress,status,pending_payload IS NOT NULL FROM catalog_editor_publications WHERE variant_id=$1 AND channel='wb'`, variant).Scan(&current, &progress, &status, &pending))
	if status != "prepared" || pending || current.Marketplace.TNVED != "0601109000" || progress.CreateAttempted || progress.Ambiguous {
		t.Fatalf("valid pending snapshot stuck behind rejected preflight: %s pending=%t %+v %+v", status, pending, current, progress)
	}
	for range 12 {
		must(ce.PublicationStep(ctx, pool, mock, "mock"))
	}
	must(pool.QueryRow(ctx, `SELECT progress,status FROM catalog_editor_publications WHERE variant_id=$1 AND channel='wb'`, variant).Scan(&progress, &status))
	if status != "created" || progress.IDs.ProductID == "" {
		t.Fatalf("promoted valid request did not finish real mock protocol: %s %+v", status, progress)
	}
	var leaked int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM product_external_ids WHERE variant_id=$1 AND provider='wildberries'`, variant).Scan(&leaked))
	if leaked != 0 {
		t.Fatal("mock protocol leaked a production link")
	}

	// An accepted upload with an unknown result is different: an uncorrelated
	// error-list message must never clear its create-attempt receipt.
	unknown := ce.Progress{Stage: "reconcile", CreateAttempted: true, Ambiguous: true}
	unknownRaw, _ := json.Marshal(unknown)
	_, err = pool.Exec(ctx, `UPDATE catalog_editor_publications SET payload=$2,fingerprint='old',progress=$3,status='prepared',pending_payload=$4,pending_fingerprint='new',next_attempt_at=NOW() WHERE variant_id=$1 AND channel='wb'`, variant, oldRaw, unknownRaw, nextRaw)
	must(err)
	boundary := editorPublisherFunc(func(_ context.Context, _ string, _ ce.Request, p ce.Progress) (ce.Result, error) {
		return ce.Result{Status: "failed", Progress: p, Message: "unknown upload result"}, nil
	})
	must(ce.PublicationStep(ctx, pool, boundary, "mock"))
	must(pool.QueryRow(ctx, `SELECT payload,progress,status,pending_payload IS NOT NULL FROM catalog_editor_publications WHERE variant_id=$1 AND channel='wb'`, variant).Scan(&current, &progress, &status, &pending))
	if status != "failed" || !pending || current.Marketplace.TNVED != "invalid" || !progress.CreateAttempted || !progress.Ambiguous {
		t.Fatal("unknown upload was silently turned into another creation attempt")
	}
}
