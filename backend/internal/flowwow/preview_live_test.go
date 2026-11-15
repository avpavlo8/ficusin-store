package flowwow

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLinkPreviewLocalDatabase(t *testing.T) {
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
	if e = pool.QueryRow(ctx, "SELECT current_database()").Scan(&db); e != nil {
		t.Fatal(e)
	}
	if db != "ficusin_local_publisher_test" {
		t.Skip("isolated publisher database required")
	}
	if e = migrate.Apply(ctx, pool, "../../../timeweb/migrations"); e != nil {
		t.Fatal(e)
	}
	tag := fmt.Sprint(time.Now().UnixNano())
	sku := tag
	var product, variant int64
	if e = pool.QueryRow(ctx, `INSERT INTO products(name,slug) VALUES('Stage plant',$1) RETURNING id`, "preview-"+tag).Scan(&product); e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, product)
	if e = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,sku,label,base_price_minor) VALUES($1,$2,'D10',99900) RETURNING id`, product, sku).Scan(&variant); e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, `DELETE FROM flowwow_link_preview`)
	s := &Service{Pool: pool, Mode: "mock", Client: integration.NewMockFlowwowClient()}
	source := LinkSnapshot{ShopID: "255831", CapturedAt: time.Now().UTC(), Items: []PreviewLink{{SKU: sku, Name: "Production plant", RemoteID: "77577298", OfferID: "210", PriceMinor: 55000, Stock: 40, Status: "synced", Message: "Source synchronized"}}}
	if e = ImportLinkPreview(ctx, pool, "staging", "mock", db, source, false); e != nil {
		t.Fatal(e)
	}
	preview, e := s.LinkPreview(ctx)
	if e != nil || preview != nil {
		t.Fatalf("dry-run stored data: %+v %v", preview, e)
	}
	for _, args := range [][3]string{{"production", "mock", db}, {"staging", "live", db}, {"staging", "mock", "wrong_test"}} {
		if e = ImportLinkPreview(ctx, pool, args[0], args[1], args[2], source, true); e == nil {
			t.Fatal("unsafe target accepted", args)
		}
	}
	if e = ImportLinkPreview(ctx, pool, "staging", "mock", db, source, true); e != nil {
		t.Fatal(e)
	}
	preview, e = s.LinkPreview(ctx)
	if e != nil || preview == nil || len(preview.Items) != 1 || preview.Items[0].ProductID != product || preview.Items[0].PriceMinor != 55000 || preview.Items[0].RemoteID != "77577298" {
		t.Fatalf("preview mismatch: %+v %v", preview, e)
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM catalog_editor_remote_links WHERE variant_id=$1)+(SELECT COUNT(*) FROM catalog_editor_publications WHERE variant_id=$1)+(SELECT COUNT(*) FROM flowwow_inventory_sync WHERE variant_id=$1)`, variant).Scan(&count); e != nil || count != 0 {
		t.Fatalf("snapshot became an executable link: %d %v", count, e)
	}
	if e = s.InventoryStep(ctx); e != nil {
		t.Fatal(e)
	}
	s.Mode = "live"
	preview, e = s.LinkPreview(ctx)
	if e != nil || preview != nil {
		t.Fatal("production exposed staging preview", preview, e)
	}
	s.Mode = "mock"
	older := source
	older.CapturedAt = source.CapturedAt.Add(-time.Hour)
	if e = ImportLinkPreview(ctx, pool, "staging", "mock", db, older, true); e == nil {
		t.Fatal("older observation replaced fresh snapshot")
	}
	duplicate := source
	duplicate.Items = append(append([]PreviewLink{}, source.Items...), source.Items[0])
	if e = ImportLinkPreview(ctx, pool, "staging", "mock", db, duplicate, true); e == nil {
		t.Fatal("duplicate identities accepted")
	}
}
