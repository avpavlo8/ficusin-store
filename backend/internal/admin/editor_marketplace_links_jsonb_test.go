package admin

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestEditorMarketplaceLinksExcludeSabyReceiptsPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Session-local relations shadow the real names. No existing catalogue,
	// receipts, settings or live external IDs are changed by this regression.
	_, err = tx.Exec(ctx, `CREATE TEMP TABLE catalog_editor_remote_links(variant_id BIGINT,channel TEXT,execution_mode TEXT,remote_id TEXT,offer_id TEXT) ON COMMIT DROP;
CREATE TEMP TABLE canonical_product_directory(variant_id BIGINT,wb_nm_ids TEXT[],wb_articles TEXT[],ozon_articles TEXT[]) ON COMMIT DROP;
INSERT INTO catalog_editor_remote_links VALUES (10,'saby','live','3980','X-SABY'),(10,'ozon','live','6975452207','ozon-article'),(10,'wb','live','123','wb-article'),(10,'ozon','mock','mock-id','mock-article');
INSERT INTO canonical_product_directory VALUES (10,ARRAY['canonical-wb'],ARRAY['canonical-wb-article'],ARRAY['canonical-ozon']);`)
	if err != nil {
		t.Fatal(err)
	}
	links, err := editorLinkedChannels(ctx, tx, 10, "live")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 || links["wb"] != "123" || links["ozon"] != "ozon-article" {
		t.Fatalf("wrong marketplace links: %v", links)
	}
	var sabyCount int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM catalog_editor_remote_links WHERE channel='saby'`).Scan(&sabyCount); err != nil || sabyCount != 1 {
		t.Fatalf("Saby receipt changed: %d/%v", sabyCount, err)
	}
}
